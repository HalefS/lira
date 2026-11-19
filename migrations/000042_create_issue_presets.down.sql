-- Rolling back loses every manager-authored preset and its consumable lines.
-- They are configuration, not issue history: issues keep their own copies of
-- problem, resolution and consumables, so no past incident changes here.
DROP TABLE IF EXISTS issue_preset_consumables;
DROP TABLE IF EXISTS issue_presets;
