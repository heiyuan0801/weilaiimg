package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type videoInspection struct {
	Metadata  map[string]any
	Width     *int
	Height    *int
	Duration  *float64
	Thumbnail []byte
	Status    string
	Error     string
}

// inspectVideo performs a bounded, synchronous best-effort inspection. The
// original upload is usable even when ffprobe/ffmpeg are absent; metadata then
// records the exact pending boundary for a future worker.
func inspectVideo(ctx context.Context, name, mimeType string, data []byte) videoInspection {
	result := videoInspection{Metadata: map[string]any{}, Status: "ready"}
	if !strings.HasPrefix(strings.ToLower(mimeType), "video/") {
		return result
	}
	result.Metadata["media_type"] = "video"
	result.Metadata["processing_status"] = "pending"
	result.Metadata["original_name"] = name
	ffprobe, probeErr := exec.LookPath("ffprobe")
	ffmpeg, ffmpegErr := exec.LookPath("ffmpeg")
	if probeErr != nil {
		result.Metadata["processing_status"] = "unavailable"
		result.Metadata["processing_error"] = "ffprobe is not installed; video metadata worker is pending"
		if ffmpegErr != nil {
			result.Metadata["thumbnail_status"] = "unavailable"
			result.Metadata["thumbnail_error"] = "ffmpeg is not installed; first-frame extraction is pending"
		}
		result.Error = "video metadata unavailable: ffprobe is not installed"
		return result
	}

	temp, err := os.CreateTemp("", "imagehub-video-*")
	if err != nil {
		return videoInspectionFailure(result, "could not create temporary video file")
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if _, err = temp.Write(data); err != nil {
		temp.Close()
		return videoInspectionFailure(result, "could not write temporary video file")
	}
	if err = temp.Close(); err != nil {
		return videoInspectionFailure(result, "could not close temporary video file")
	}

	probeCmd := exec.CommandContext(ctx, ffprobe, "-v", "error", "-show_entries", "format=duration:stream=codec_type,codec_name,width,height", "-of", "json", tempName)
	probeOutput, err := probeCmd.Output()
	if err != nil {
		result.Metadata["processing_status"] = "failed"
		result.Metadata["processing_error"] = "ffprobe failed"
		result.Error = "video metadata inspection failed"
	} else {
		var probe struct {
			Streams []struct {
				CodecType string `json:"codec_type"`
				CodecName string `json:"codec_name"`
				Width     int    `json:"width"`
				Height    int    `json:"height"`
			} `json:"streams"`
			Format struct {
				Duration string `json:"duration"`
			} `json:"format"`
		}
		if json.Unmarshal(probeOutput, &probe) != nil {
			result.Metadata["processing_status"] = "failed"
			result.Metadata["processing_error"] = "ffprobe returned invalid JSON"
			result.Error = "video metadata inspection returned invalid data"
		} else {
			result.Metadata["processing_status"] = "ready"
			for _, stream := range probe.Streams {
				if stream.CodecType == "video" {
					result.Metadata["video_codec"] = stream.CodecName
					if stream.Width > 0 {
						value := stream.Width
						result.Width = &value
						result.Metadata["width"] = value
					}
					if stream.Height > 0 {
						value := stream.Height
						result.Height = &value
						result.Metadata["height"] = value
					}
					break
				}
			}
			if probe.Format.Duration != "" {
				if duration, parseErr := strconv.ParseFloat(probe.Format.Duration, 64); parseErr == nil && duration >= 0 {
					result.Duration = &duration
					result.Metadata["duration_seconds"] = duration
				}
			}
		}
	}

	if ffmpegErr != nil {
		result.Metadata["thumbnail_status"] = "unavailable"
		result.Metadata["thumbnail_error"] = "ffmpeg is not installed; first-frame extraction is pending"
		if result.Error == "" {
			result.Error = "video thumbnail unavailable: ffmpeg is not installed"
		}
		return result
	}
	thumbnailCmd := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-loglevel", "error", "-i", tempName, "-vf", "thumbnail,scale=640:-2", "-frames:v", "1", "-f", "image2", "pipe:1")
	thumbnail, err := thumbnailCmd.Output()
	if err != nil || len(thumbnail) == 0 {
		result.Metadata["thumbnail_status"] = "failed"
		result.Metadata["thumbnail_error"] = "ffmpeg first-frame extraction failed"
		if result.Error == "" {
			result.Error = "video thumbnail extraction failed"
		}
		return result
	}
	result.Thumbnail = thumbnail
	result.Metadata["thumbnail_status"] = "ready"
	return result
}

func videoInspectionFailure(result videoInspection, message string) videoInspection {
	result.Metadata["processing_status"] = "failed"
	result.Metadata["processing_error"] = message
	result.Error = message
	return result
}

func (s *server) mediaThumbnail(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		http.NotFound(w, r)
		return
	}
	if !s.mediaHostAllowed(r.Context(), r.Host) {
		http.Error(w, "custom domain is not verified", http.StatusMisdirectedRequest)
		return
	}
	var key, mimeType, visibility, ownerID, linkTokenHash, teamID string
	var guestExpiresAt *time.Time
	if err := s.db.QueryRow(r.Context(), `SELECT thumbnail_object_key,mime_type,visibility,COALESCE(owner_id::text,''),COALESCE(link_token_hash,''),guest_expires_at,COALESCE(team_id::text,'') FROM images WHERE id=$1 AND deleted_at IS NULL`, r.PathValue("id")).Scan(&key, &mimeType, &visibility, &ownerID, &linkTokenHash, &guestExpiresAt, &teamID); err != nil || key == "" {
		http.NotFound(w, r)
		return
	}
	guestTokenValid := linkTokenHash != "" && tokenDigest(r.URL.Query().Get("token")) == linkTokenHash && (guestExpiresAt == nil || guestExpiresAt.After(time.Now()))
	if visibility == "link" && guestTokenValid { /* signed link */
	} else if visibility != "public" {
		u, ok := s.currentUser(r)
		teamMember := false
		if ok && teamID != "" {
			_ = s.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM team_members WHERE team_id=$1 AND user_id=$2)`, teamID, u.ID).Scan(&teamMember)
		}
		if !ok || (u.ID != ownerID && !teamMember) {
			http.Error(w, "forbidden", 403)
			return
		}
	}
	// Thumbnail files use the same backend as the original image. This keeps
	// media readable after an administrator changes the active storage backend.
	var backend, channelID string
	if err := s.db.QueryRow(r.Context(), `SELECT storage_backend,COALESCE(storage_channel,'') FROM images WHERE id=$1 AND deleted_at IS NULL`, r.PathValue("id")).Scan(&backend, &channelID); err != nil {
		http.NotFound(w, r)
		return
	}
	reader, err := s.storageForChannel(backend, channelID).Open(r.Context(), key)
	if err != nil {
		http.Error(w, "thumbnail unavailable", 502)
		return
	}
	defer reader.Close()
	w.Header().Set("Content-Type", "image/jpeg")
	if visibility == "public" {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "private, no-store")
	}
	_, _ = io.Copy(w, reader)
	_ = mimeType
}
