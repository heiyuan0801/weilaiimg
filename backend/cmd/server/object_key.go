package main

import (
	"crypto/md5" // #nosec G501 -- MD5 is exposed only as an optional filename token, never for integrity.
	"crypto/rand"
	"encoding/hex"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var objectKeyUnsafe = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

// objectKeyForUpload expands the administrator's upload naming policy. The
// content hash remains the deduplication key; this path only controls the
// physical object name created for a new object.
func objectKeyForUpload(settings map[string]any, ownerID, name, mimeType string, data []byte, sha256Hex string, now time.Time) string {
	ext := extension(name, mimeType)
	filename := strings.TrimSuffix(filepath.Base(name), filepath.Ext(filepath.Base(name)))
	filename = objectKeyUnsafe.ReplaceAllString(filename, "-")
	filename = strings.Trim(filename, ".-")
	if filename == "" {
		filename = "file"
	}
	md5sum := md5.Sum(data)
	md5Hex := hex.EncodeToString(md5sum[:])
	uuid := randomBase62(24)
	randomValue := randomBase62(settingInt(settings, "random_length", 12))
	mode := settingString(settings, "naming_mode", "sha256")
	nameToken := sha256Hex
	switch mode {
	case "original":
		// Keep the readable name while adding a content suffix so two different
		// files with the same original name can never overwrite one another.
		nameToken = filename + "-" + sha256Hex[:8]
	case "md5":
		nameToken = md5Hex
	case "uuid":
		nameToken = uuid
	case "random":
		nameToken = randomValue
	}
	vars := map[string]string{
		"year":     now.Format("2006"),
		"month":    now.Format("01"),
		"day":      now.Format("02"),
		"hour":     now.Format("15"),
		"user_id":  safeObjectPart(ownerID),
		"hash":     sha256Hex,
		"sha256":   sha256Hex,
		"md5":      md5Hex,
		"uuid":     uuid,
		"random":   randomValue,
		"filename": filename,
		"ext":      strings.TrimPrefix(ext, "."),
	}
	template := settingString(settings, "path_template", "")
	if template == "" {
		rule := settingString(settings, "directory_rule", "hash2")
		switch rule {
		case "none":
			template = "{name}.{ext}"
		case "year":
			template = "{year}/{name}.{ext}"
		case "month":
			template = "{year}/{month}/{name}.{ext}"
		case "day", "ymd":
			template = "{year}/{month}/{day}/{name}.{ext}"
		case "ym":
			template = "{year}/{month}/{name}.{ext}"
		default:
			template = "objects/{hash2}/{name}.{ext}"
		}
	}
	vars["name"] = nameToken
	vars["hash2"] = sha256Hex[:2]
	for key, value := range vars {
		template = strings.ReplaceAll(template, "{"+key+"}", value)
	}
	template = strings.ReplaceAll(template, "\\", "/")
	parts := make([]string, 0, 8)
	for _, part := range strings.Split(template, "/") {
		part = objectKeyUnsafe.ReplaceAllString(part, "-")
		part = strings.Trim(part, ".-")
		if part == "" || part == ".." {
			continue
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		return "objects/" + sha256Hex[:2] + "/" + sha256Hex + ext
	}
	result := strings.Join(parts, "/")
	if !strings.Contains(filepath.Base(result), ".") {
		result += ext
	}
	return result
}

func settingString(settings map[string]any, key, fallback string) string {
	if value, ok := settings[key].(string); ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}

func settingInt(settings map[string]any, key string, fallback int) int {
	value := 0.0
	switch raw := settings[key].(type) {
	case float64:
		value = raw
	case int:
		value = float64(raw)
	case int64:
		value = float64(raw)
	}
	if value < 4 || value > 128 {
		return fallback
	}
	return int(value)
}

func safeObjectPart(value string) string {
	value = objectKeyUnsafe.ReplaceAllString(value, "-")
	value = strings.Trim(value, ".-")
	if value == "" {
		return "guest"
	}
	return value
}

func randomBase62(length int) string {
	if length < 4 {
		length = 4
	}
	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		return strings.Repeat("0", length)
	}
	const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	result := make([]byte, length)
	for index, value := range buf {
		result[index] = alphabet[int(value)%len(alphabet)]
	}
	return string(result)
}
