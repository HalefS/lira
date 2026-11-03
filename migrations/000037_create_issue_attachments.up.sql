-- Photos attached to an issue: the fault as found and the fix as left.
--
-- WHY THE BYTES LIVE IN POSTGRES
--
-- The deployment is one binary and one database, with no shared volume
-- guaranteed between the process and any other machine. A path column would
-- point at a file that does not exist on the box answering the request, and
-- pg_dump alone would restore an issue whose evidence is gone. So the image
-- bytes are a bytea column in the same row as their metadata.
--
-- bytea also gets TOAST for free: anything over about 2 kB is moved out of the
-- heap row, so a metadata query that does not name data never reads an image.
-- STORAGE EXTERNAL is set below because these are already-compressed JPEGs and
-- PNGs; the default would make Postgres attempt pglz on every insert for no
-- gain.
--
-- Deliberately NOT base64 text, which is how users.avatar_data is stored. That
-- column costs a third more bytes and is selected by the user list and by the
-- token lookup on every authenticated request. Evidence photos must inherit
-- none of that: the bytes are only ever read by the one handler that serves a
-- single photo, and the JSON that carries an Issue never contains image data.
--
-- WHY NO SOFT DELETE
--
-- A manager deleting a photo means it is gone. Keeping the bytes behind a
-- deleted_at flag would retain guest-room images while telling the manager
-- they were removed, and would not free the storage either. The attachment
-- delete is a hard DELETE scoped by both issue id and attachment id.
--
-- WHY NO KIND OR CAPTION
--
-- The story is told by order and by the problem/resolution text on the issue.
-- A before/after flag and a free-text caption are a second taxonomy to fill in
-- on a phone in a plant room, and nothing in this version reads them. They can
-- be added without touching the bytes if the need is ever demonstrated.
CREATE TABLE IF NOT EXISTS issue_attachments (
    id          bigserial PRIMARY KEY,
    issue_id    bigint NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    created_at  timestamp(0) with time zone NOT NULL DEFAULT NOW(),

    -- Nullable so removing an account never blocks on, or deletes, the photos
    -- they took. Accounts are normally deactivated rather than deleted.
    created_by  bigint REFERENCES users(id) ON DELETE SET NULL,

    -- Written from what the server decoded and re-encoded, never from the
    -- upload's Content-Type header or from its filename.
    mime_type   text NOT NULL,
    byte_size   integer NOT NULL,
    width       integer NOT NULL,
    height      integer NOT NULL,

    -- Hex of the SHA-256 of the stored bytes, used as a strong ETag so a
    -- reopened issue costs no image fetches. Not a uniqueness key: the same
    -- picture may legitimately be attached to two issues, and a unique index
    -- would turn that into a failed upload for no gain at this size.
    sha256      text NOT NULL,

    data        bytea NOT NULL,

    CONSTRAINT issue_attachments_mime_check
        CHECK (mime_type IN ('image/jpeg', 'image/png')),
    CONSTRAINT issue_attachments_size_check
        CHECK (byte_size > 0 AND byte_size <= 8388608),
    CONSTRAINT issue_attachments_dimensions_check
        CHECK (width > 0 AND height > 0 AND width <= 10000 AND height <= 10000),
    CONSTRAINT issue_attachments_sha256_check
        CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    -- The metadata cannot drift from the bytes, even for a row inserted by hand.
    CONSTRAINT issue_attachments_data_check
        CHECK (octet_length(data) = byte_size)
);

ALTER TABLE issue_attachments ALTER COLUMN data SET STORAGE EXTERNAL;

-- The only read pattern: every photo of one issue, in upload order.
CREATE INDEX IF NOT EXISTS issue_attachments_issue_idx
    ON issue_attachments (issue_id, id);
