package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/jackc/pgx/v5"
)

// mediaWorker is a small PostgreSQL-backed queue. FOR UPDATE SKIP LOCKED lets
// multiple application replicas process jobs without a separate broker, while
// the lease recovery query makes jobs survive a crashed worker.
func (s *server) mediaWorker(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.recoverMediaLeases(ctx)
			s.processMediaJob(ctx)
		}
	}
}

func (s *server) recoverMediaLeases(ctx context.Context) {
	_, _ = s.db.Exec(ctx, `UPDATE media_jobs SET status='queued',locked_at=NULL,locked_by=NULL,available_at=now(),updated_at=now() WHERE status='running' AND locked_at < now()-interval '10 minutes'`)
}

func (s *server) processMediaJob(ctx context.Context) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return
	}
	defer tx.Rollback(ctx)
	var jobID, imageID, kind string
	err = tx.QueryRow(ctx, `SELECT id,image_id,kind FROM media_jobs WHERE status='queued' AND available_at<=now() ORDER BY created_at LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&jobID, &imageID, &kind)
	if err != nil {
		return
	}
	workerID := requestID()
	if _, err = tx.Exec(ctx, `UPDATE media_jobs SET status='running',attempts=attempts+1,locked_at=now(),locked_by=$1,updated_at=now() WHERE id=$2`, workerID, jobID); err != nil || tx.Commit(ctx) != nil {
		return
	}
	jobCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	err = s.runMediaJob(jobCtx, imageID, kind)
	if err == nil {
		_, _ = s.db.Exec(ctx, `UPDATE media_jobs SET status='succeeded',locked_at=NULL,locked_by=NULL,updated_at=now() WHERE id=$1`, jobID)
		return
	}
	_, _ = s.db.Exec(ctx, `UPDATE media_jobs SET status=CASE WHEN attempts>=5 THEN 'failed' ELSE 'queued' END,available_at=now()+make_interval(secs => LEAST(300,attempts*15)),locked_at=NULL,locked_by=NULL,last_error=$1,updated_at=now() WHERE id=$2`, err.Error(), jobID)
}

func (s *server) runMediaJob(ctx context.Context, imageID, kind string) error {
	if kind != "video_inspect" && kind != "video_thumbnail" {
		return fmt.Errorf("unsupported media job %q", kind)
	}
	var key, backend, channelID, name, mimeType string
	if err := s.db.QueryRow(ctx, `SELECT object_key,storage_backend,COALESCE(storage_channel,''),original_name,mime_type FROM images WHERE id=$1 AND deleted_at IS NULL`, imageID).Scan(&key, &backend, &channelID, &name, &mimeType); err != nil {
		return err
	}
	r, err := s.storageForChannel(backend, channelID).Open(ctx, key)
	if err != nil {
		return err
	}
	data, err := io.ReadAll(io.LimitReader(r, 256<<20))
	_ = r.Close()
	if err != nil {
		return err
	}
	inspection := inspectVideo(ctx, name, mimeType, data)
	meta, _ := json.Marshal(inspection.Metadata)
	thumbKey := ""
	if len(inspection.Thumbnail) > 0 {
		thumbKey = key + ".thumb.jpg"
		if _, err = s.storageForChannel(backend, channelID).Put(ctx, thumbKey, bytes.NewReader(inspection.Thumbnail), int64(len(inspection.Thumbnail)), "image/jpeg"); err != nil {
			return err
		}
	}
	_, err = s.db.Exec(ctx, `UPDATE images SET width=$1,height=$2,duration_seconds=$3,status=$4,thumbnail_object_key=NULLIF($5,''),processing_error=NULLIF($6,''),metadata=$7 WHERE id=$8`, inspection.Width, inspection.Height, inspection.Duration, inspection.Status, thumbKey, inspection.Error, meta, imageID)
	return err
}

func (s *server) enqueueMediaJob(ctx context.Context, imageID, kind string) {
	if s.db == nil {
		return
	}
	_, _ = s.db.Exec(ctx, `INSERT INTO media_jobs(image_id,kind) VALUES($1,$2) ON CONFLICT(image_id,kind) DO UPDATE SET status='queued',available_at=now(),updated_at=now()`, imageID, kind)
}

var _ = pgx.ErrNoRows
