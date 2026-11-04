ALTER TABLE videos
    DROP COLUMN video_source,
    DROP COLUMN final_container,
    DROP COLUMN final_audio_codec,
    DROP COLUMN final_video_codec,
    DROP COLUMN original_container,
    DROP COLUMN original_audio_codec,
    DROP COLUMN original_video_codec,
    DROP COLUMN duration_seconds;

DROP TYPE video_container;
DROP TYPE audio_codec;
DROP TYPE video_codec;
DROP TYPE video_source;
