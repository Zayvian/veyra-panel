-- Calendar dates without a clock are surprising for a paid service. Existing
-- user schedules keep their old meaning: midnight reset and expiry at the end
-- of the selected local day.
ALTER TABLE users ADD COLUMN reset_hour INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN reset_minute INTEGER NOT NULL DEFAULT 0;

-- The dashboard's node ledger is operational accounting, separate from a
-- user's quota. A node can have its own monthly reporting period without
-- deleting the immutable daily traffic history.
ALTER TABLE nodes ADD COLUMN stats_reset_day INTEGER NOT NULL DEFAULT 0;
ALTER TABLE nodes ADD COLUMN stats_reset_hour INTEGER NOT NULL DEFAULT 0;
ALTER TABLE nodes ADD COLUMN stats_reset_minute INTEGER NOT NULL DEFAULT 0;
ALTER TABLE nodes ADD COLUMN stats_up INTEGER NOT NULL DEFAULT 0;
ALTER TABLE nodes ADD COLUMN stats_down INTEGER NOT NULL DEFAULT 0;
ALTER TABLE nodes ADD COLUMN stats_last_reset_at INTEGER;

-- Before a node gets a monthly period, its new "current period" counter should
-- mean the same thing the old overview total meant: all recorded traffic.
UPDATE nodes
SET stats_up = COALESCE((SELECT sum(up) FROM traffic WHERE traffic.node_id = nodes.id), 0),
    stats_down = COALESCE((SELECT sum(down) FROM traffic WHERE traffic.node_id = nodes.id), 0);
