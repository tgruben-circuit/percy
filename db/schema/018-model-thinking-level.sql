-- Add thinking_level to models table
-- Values: "off", "minimal", "low", "medium", "high"
ALTER TABLE models ADD COLUMN thinking_level TEXT NOT NULL DEFAULT 'medium';
