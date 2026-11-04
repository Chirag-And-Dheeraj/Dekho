CREATE TABLE outbox_events (
    event_id TEXT PRIMARY KEY,
    aggregate_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_at TIMESTAMPTZ
);

CREATE INDEX outbox_events_unpublished_idx
    ON outbox_events (created_at, event_id)
    WHERE published_at IS NULL;

CREATE INDEX outbox_events_published_idx
    ON outbox_events (published_at)
    WHERE published_at IS NOT NULL;

CREATE FUNCTION notify_outbox_event_inserted() RETURNS trigger AS $$
BEGIN
    PERFORM pg_notify('dekho_outbox_events', NEW.event_id);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER outbox_events_notify_insert
    AFTER INSERT ON outbox_events
    FOR EACH ROW EXECUTE FUNCTION notify_outbox_event_inserted();
