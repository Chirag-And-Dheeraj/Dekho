package storagekeys

import "path"

const (
	RawFilesFolder   = "raw_files"
	HLSChunksFolder  = "hls_chunks"
	ManifestsFolder  = "manifests"
	ThumbnailsFolder = "thumbnails"
)

func RawFile(videoID, extension string) string {
	return path.Join(RawFilesFolder, videoID+extension)
}

func HLSChunk(videoID, fileName string) string {
	return path.Join(HLSChunksFolder, videoID, fileName)
}

func HLSChunkPrefix(videoID string) string {
	return path.Join(HLSChunksFolder, videoID) + "/"
}

func Manifest(videoID string) string {
	return path.Join(ManifestsFolder, videoID+".m3u8")
}

func Thumbnail(videoID string) string {
	return path.Join(ThumbnailsFolder, videoID+".png")
}
