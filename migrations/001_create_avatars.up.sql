CREATE TABLE avatars (
    id UUID PRIMARY KEY,
    user_id VARCHAR(255) NOT NULL,

    file_name VARCHAR(255) NOT NULL,
    mime_type VARCHAR(100) NOT NULL,
    size_bytes BIGINT NOT NULL,
    width INTEGER NOT NULL,
    height INTEGER NOT NULL,

    s3_key VARCHAR(1024) NOT NULL,
    thumbnail_s3_keys JSONB,

    upload_status VARCHAR(50) NOT NULL DEFAULT 'uploading',
    processing_status VARCHAR(50) NOT NULL DEFAULT 'pending',
    processing_message_id UUID,

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMPTZ,

    CONSTRAINT avatars_user_id_not_empty CHECK (length(user_id) > 0),
    CONSTRAINT avatars_file_name_not_empty CHECK (length(file_name) > 0),
    CONSTRAINT avatars_mime_type_check CHECK (mime_type IN ('image/jpeg', 'image/png', 'image/webp')),
    CONSTRAINT avatars_size_bytes_positive CHECK (size_bytes > 0),
    CONSTRAINT avatars_width_positive CHECK (width > 0),
    CONSTRAINT avatars_height_positive CHECK (height > 0),
    CONSTRAINT avatars_s3_key_not_empty CHECK (length(s3_key) > 0),
    CONSTRAINT avatars_thumbnail_s3_keys_object CHECK (thumbnail_s3_keys IS NULL OR jsonb_typeof(thumbnail_s3_keys) = 'object'),
    CONSTRAINT avatars_upload_status_check CHECK (upload_status IN ('uploading', 'completed', 'failed')),
    CONSTRAINT avatars_processing_status_check CHECK (processing_status IN ('pending', 'processing', 'completed', 'failed'))
);

CREATE INDEX idx_avatars_user_id ON avatars (user_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_avatars_status ON avatars (upload_status, processing_status);
