-- The CDN (vless-ws) inbound and the China probe were removed. A leftover
-- vless-ws row would be an unsupported protocol, which fails every
-- subscription that includes it, so those inbounds go too, the same way a
-- manual delete would. Fresh installs never had the probe table.
DELETE FROM inbounds WHERE protocol = 'vless-ws';
DROP TABLE IF EXISTS probe_nodes;
