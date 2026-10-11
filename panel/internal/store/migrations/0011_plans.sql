-- Plans are presets for the user form, nothing more: creating a user from one
-- copies its values, and nothing links the user back to it afterwards. Editing
-- or deleting a plan therefore never changes an account someone has paid for.
CREATE TABLE plans (
    id            INTEGER PRIMARY KEY,
    name          TEXT    NOT NULL UNIQUE,
    days          INTEGER NOT NULL DEFAULT 0,    -- validity; 0 = no expiry
    traffic_limit INTEGER NOT NULL DEFAULT 0,    -- bytes; 0 = unlimited
    ip_limit      INTEGER NOT NULL DEFAULT 0,    -- 0 = unlimited
    reset_day     INTEGER NOT NULL DEFAULT 0,    -- 0 = never, -1 = the day the user is created, else 1-31
    note          TEXT    NOT NULL DEFAULT '',
    created_at    INTEGER NOT NULL
);
