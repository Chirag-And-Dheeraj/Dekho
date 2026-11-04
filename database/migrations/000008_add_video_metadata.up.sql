CREATE TYPE video_source AS ENUM ('upload_form', 'url_import', 'api_import', 'other');
CREATE TYPE video_codec AS ENUM ('h264', 'hevc', 'vp9', 'av1', 'mpeg4', 'mpeg2video', 'other', 'unknown', 'none');
CREATE TYPE audio_codec AS ENUM ('aac', 'mp3', 'opus', 'vorbis', 'ac3', 'eac3', 'pcm', 'other', 'unknown', 'none');
CREATE TYPE video_container AS ENUM ('mp4', 'matroska', 'mov', 'webm', 'mpegts', 'hls', 'other', 'unknown');

ALTER TABLE videos
    ADD COLUMN duration_seconds NUMERIC(12,3) CHECK (duration_seconds IS NULL OR duration_seconds >= 0),
    ADD COLUMN original_video_codec video_codec,
    ADD COLUMN original_audio_codec audio_codec,
    ADD COLUMN original_container video_container,
    ADD COLUMN final_video_codec video_codec,
    ADD COLUMN final_audio_codec audio_codec,
    ADD COLUMN final_container video_container,
    ADD COLUMN video_source video_source NOT NULL DEFAULT 'upload_form';

ALTER TABLE videos ALTER COLUMN video_source DROP DEFAULT;
