-- Which core serves an inbound on its node: '' is sing-box, which is every
-- inbound that existed before this column; 'xray' runs it in Xray beside
-- sing-box. Fixed when the inbound is created.
ALTER TABLE inbounds ADD COLUMN core TEXT NOT NULL DEFAULT '';
