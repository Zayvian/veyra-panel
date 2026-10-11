-- Fixed-point thousandths: 1000 = 1x, 0 = free traffic.
ALTER TABLE nodes ADD COLUMN rate_milli INTEGER NOT NULL DEFAULT 1000 CHECK(rate_milli BETWEEN 0 AND 100000);
ALTER TABLE users ADD COLUMN traffic_up INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN traffic_down INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN traffic_up_remainder INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN traffic_down_remainder INTEGER NOT NULL DEFAULT 0;
-- Historical quota usage has no reliable direction across past resets.
-- Preserve its previous subscription representation rather than inventing a split.
UPDATE users SET traffic_down = traffic_used;
