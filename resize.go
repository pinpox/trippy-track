package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/disintegration/imaging"
)

const (
	ThumbWidth  = 400
	MediumWidth = 1200
)

// GenerateThumbnails creates _thumb and _med versions of an image.
// Videos are skipped. Returns the base filename (unchanged).
func GenerateThumbnails(uploadsDir, filename string) {
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == ".mp4" || ext == ".webm" || ext == ".mov" || ext == ".avi" {
		return // Skip videos
	}

	srcPath := filepath.Join(uploadsDir, filename)
	base := strings.TrimSuffix(filename, filepath.Ext(filename))

	src, err := imaging.Open(srcPath, imaging.AutoOrientation(true))
	if err != nil {
		log.Printf("resize: failed to open %s: %v", filename, err)
		return
	}

	// Thumbnail (400px wide)
	thumb := imaging.Resize(src, ThumbWidth, 0, imaging.Lanczos)
	thumbPath := filepath.Join(uploadsDir, base+"_thumb"+ext)
	if err := imaging.Save(thumb, thumbPath); err != nil {
		log.Printf("resize: failed to save thumb %s: %v", thumbPath, err)
	}

	// Medium (1200px wide) — only if original is wider
	if src.Bounds().Dx() > MediumWidth {
		med := imaging.Resize(src, MediumWidth, 0, imaging.Lanczos)
		medPath := filepath.Join(uploadsDir, base+"_med"+ext)
		if err := imaging.Save(med, medPath); err != nil {
			log.Printf("resize: failed to save medium %s: %v", medPath, err)
		}
	} else {
		// Original is smaller than medium — just copy as medium
		medPath := filepath.Join(uploadsDir, base+"_med"+ext)
		data, err := os.ReadFile(srcPath)
		if err == nil {
			os.WriteFile(medPath, data, 0o644)
		}
	}
}

// ThumbPath returns the thumbnail path for a filename.
func ThumbPath(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == ".mp4" || ext == ".webm" || ext == ".mov" || ext == ".avi" {
		return filename // Videos have no thumbnails
	}
	base := strings.TrimSuffix(filename, filepath.Ext(filename))
	return base + "_thumb" + ext
}

// MediumPath returns the medium-res path for a filename.
func MediumPath(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == ".mp4" || ext == ".webm" || ext == ".mov" || ext == ".avi" {
		return filename
	}
	base := strings.TrimSuffix(filename, filepath.Ext(filename))
	return base + "_med" + ext
}

// TranscodeVideo re-encodes a video to a universally compatible H.264 format.
// The original file is replaced in-place. This ensures videos recorded on mobile
// devices play correctly across all browsers.
func TranscodeVideo(uploadsDir, filename string) {
	ext := strings.ToLower(filepath.Ext(filename))
	if ext != ".mp4" && ext != ".webm" && ext != ".mov" && ext != ".avi" {
		return
	}

	srcPath := filepath.Join(uploadsDir, filename)
	base := strings.TrimSuffix(filename, filepath.Ext(filename))
	tmpPath := filepath.Join(uploadsDir, base+"_transcoding.mp4")

	cmd := exec.Command("ffmpeg",
		"-i", srcPath,
		"-c:v", "libx264",
		"-preset", "ultrafast",
		"-crf", "18",
		"-profile:v", "main",
		"-pix_fmt", "yuv420p",
		"-vf", "scale=1920:1920:force_original_aspect_ratio=decrease:force_divisible_by=2",
		"-movflags", "+faststart",
		"-c:a", "aac",
		"-y",
		tmpPath,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("transcode: failed to transcode %s: %v\n%s", filename, err, output)
		os.Remove(tmpPath)
		return
	}

	if err := os.Rename(tmpPath, srcPath); err != nil {
		log.Printf("transcode: failed to replace %s: %v", filename, err)
		os.Remove(tmpPath)
		return
	}

	log.Printf("transcode: %s done", filename)
}

// BackfillThumbnails generates thumbnails for all existing photos that don't have them.
func BackfillThumbnails(uploadsDir string) {
	entries, err := os.ReadDir(uploadsDir)
	if err != nil {
		log.Printf("backfill: failed to read uploads dir: %v", err)
		return
	}

	count := 0
	for _, entry := range entries {
		name := entry.Name()
		if strings.Contains(name, "_thumb") || strings.Contains(name, "_med") {
			continue
		}
		ext := strings.ToLower(filepath.Ext(name))
		if ext == ".mp4" || ext == ".webm" || ext == ".mov" || ext == ".avi" {
			continue
		}
		if ext != ".jpg" && ext != ".jpeg" && ext != ".png" && ext != ".gif" && ext != ".webp" {
			continue
		}

		base := strings.TrimSuffix(name, filepath.Ext(name))
		thumbPath := filepath.Join(uploadsDir, base+"_thumb"+ext)
		if _, err := os.Stat(thumbPath); err == nil {
			continue // Already has thumbnail
		}

		fmt.Printf("backfill: generating thumbnails for %s\n", name)
		GenerateThumbnails(uploadsDir, name)
		count++
	}
	if count > 0 {
		log.Printf("backfill: generated thumbnails for %d images", count)
	}
}
