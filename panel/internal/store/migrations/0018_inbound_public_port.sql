-- The port this inbound is reachable on at its node's address, when that is
-- not the port it listens on. A NAT VPS forwards some provider-assigned
-- external port to the inbound's internal one: clients and relays must dial
-- the external port, the node must still listen on the internal one.
--
-- 0 means the two are the same, which is every host with its own address.
ALTER TABLE inbounds ADD COLUMN public_port INTEGER NOT NULL DEFAULT 0;
