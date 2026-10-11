-- Protocol 2 (stats_ack): the last stats report committed for each node, as
-- the node numbers them. A node resends a report until the panel acknowledges
-- it, and the acknowledgement can be lost after the commit; these let the
-- resend be recognised and acknowledged without billing it a second time. The
-- epoch is one per node process, so a restarted node starts a new sequence.
ALTER TABLE nodes ADD COLUMN stats_epoch TEXT NOT NULL DEFAULT '';
ALTER TABLE nodes ADD COLUMN stats_seq INTEGER NOT NULL DEFAULT 0;
