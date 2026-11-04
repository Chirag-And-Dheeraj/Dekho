DROP TRIGGER IF EXISTS outbox_events_notify_insert ON outbox_events;
DROP FUNCTION IF EXISTS notify_outbox_event_inserted();
DROP INDEX IF EXISTS outbox_events_published_idx;
DROP INDEX IF EXISTS outbox_events_unpublished_idx;
DROP TABLE IF EXISTS outbox_events;
