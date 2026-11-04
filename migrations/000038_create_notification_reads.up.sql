-- The notification centre stores no notifications.
--
-- Every item is derived at read time from the tables that already describe the
-- work: maintenance_schedules, maintenance_checks, recurring_alerts,
-- tv_swap_alerts, consumable_items and issues. What cannot be derived is
-- whether a particular person has already looked at one. That is all this table
-- holds.
--
-- WHY THE KEY IS A TUPLE, NOT A TEXT KEY
--
-- The identity of a feed item is (kind, source_id, variant). A text key such as
-- 'maintenance:12' invites prefix parsing, collides across kinds, and cannot
-- carry the one field that matters for low stock -- the reorder level the item
-- fell below. An item that goes low at level 6, is read, is restocked and later
-- falls below level 10 is a new alert; keying only on the item id would swallow
-- it.
--
-- MARK-ALL IS ONE SENTINEL ROW
--
-- The row ('*', 0, '') means "everything that existed when this was written has
-- been seen". Unread is then one comparison per item:
--
--     GREATEST(item.read_at, sentinel.read_at) < occurred_at
--
-- with '-infinity' standing in for "never read". That is why there is no unread
-- counter to drift: a counter would require every reader to also be a writer,
-- and every request for the sidebar badge to take a row lock.
--
-- WHY read_at HAS MILLISECONDS
--
-- The source columns are timestamp(0). The comparison is `read_at < occurred_at`
-- on the unread side (equal means read), so a second-truncated mark cannot leave
-- an item unread for ever. The finer mark side narrows the window in which a
-- mark-all issued at 12:00:00.7 appears to cover an item created at 12:00:00.9.
--
-- NO CHECK ON kind
--
-- The vocabulary belongs to the feed. A constraint would turn every future kind
-- into a migration -- exactly the coupling that a derived feed exists to avoid.
CREATE TABLE IF NOT EXISTS notification_reads (
    user_id   bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind      text   NOT NULL,
    source_id bigint NOT NULL,
    variant   text   NOT NULL DEFAULT '',
    read_at   timestamp(3) with time zone NOT NULL DEFAULT NOW(),

    CONSTRAINT notification_reads_pkey
        PRIMARY KEY (user_id, kind, source_id, variant),

    -- A sentinel is the only row allowed source_id 0, and a real item is the
    -- only row allowed a kind other than '*'. Together they keep the sentinel
    -- from being confused with an item whose id happens to be zero.
    CONSTRAINT notification_reads_sentinel_check CHECK (
        (kind =  '*' AND source_id = 0 AND variant = '') OR
        (kind <> '*' AND source_id > 0)
    )
);

-- No secondary index. Every read is a lookup by the full primary key: the feed
-- joins item identity to (user_id, kind, source_id, variant), and mark-all
-- upserts one known tuple. An index on read_at would only serve a query nobody
-- makes.
