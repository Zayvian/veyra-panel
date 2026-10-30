-- An account with neither a reset day nor an expiry now resets monthly on the
-- anniversary of its creation. Its last_reset_at is still the creation time,
-- so without this every such account would be found overdue and wiped on the
-- first sweep after the upgrade. Start the cycle now instead: the first reset
-- happens on the next anniversary.
UPDATE users SET last_reset_at = unixepoch()
WHERE reset_day = 0 AND expires_at IS NULL;
