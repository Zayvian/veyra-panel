-- Nodes the operator marked as domestic probes: machines in China that test
-- whether the other nodes are reachable from there. A table of its own rather
-- than a column, because it is a role a few nodes have, not a property of all.
CREATE TABLE probe_nodes (
    node_id INTEGER PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE
);
