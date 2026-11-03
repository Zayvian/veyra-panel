-- Hysteria2's salamander obfuscation was removed. An inbound that had it on
-- loses it on both sides at once: the node stops expecting it, and the
-- subscription stops handing out its password. Clients on such an inbound
-- connect again once they update their subscription.
UPDATE inbounds
SET config = json_remove(config, '$.obfs'),
    client = json_remove(client, '$.obfs')
WHERE protocol = 'hysteria2'
  AND (json_extract(config, '$.obfs') IS NOT NULL OR json_extract(client, '$.obfs') IS NOT NULL);
