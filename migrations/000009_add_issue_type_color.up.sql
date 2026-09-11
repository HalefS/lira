ALTER TABLE issue_types ADD COLUMN IF NOT EXISTS color text NOT NULL DEFAULT '';

-- Give the 7 default types the exact colors they already had client-side,
-- so upgrading doesn't change how anything looks.
UPDATE issue_types SET color = 'badge-door'     WHERE name = 'Door'     AND color = '';
UPDATE issue_types SET color = 'badge-internet' WHERE name = 'Internet' AND color = '';
UPDATE issue_types SET color = 'badge-hardware' WHERE name = 'Hardware' AND color = '';
UPDATE issue_types SET color = 'badge-violet'   WHERE name = 'TV'       AND color = '';
UPDATE issue_types SET color = 'badge-sky'      WHERE name = 'AC'       AND color = '';
UPDATE issue_types SET color = 'badge-rose'     WHERE name = 'Phone'    AND color = '';
UPDATE issue_types SET color = 'badge-slate'    WHERE name = 'Other'    AND color = '';

-- Any other pre-existing custom type (added before this migration) gets
-- the next colors in the palette, cycling if there are more rows than
-- palette entries.
WITH palette(idx, cls) AS (
    VALUES
        (0,  'badge-door'),     (1,  'badge-internet'), (2,  'badge-hardware'),
        (3,  'badge-violet'),   (4,  'badge-sky'),       (5,  'badge-rose'),
        (6,  'badge-slate'),    (7,  'badge-orange'),    (8,  'badge-fuchsia'),
        (9,  'badge-red'),      (10, 'badge-yellow'),    (11, 'badge-lime'),
        (12, 'badge-emerald'),  (13, 'badge-cyan'),      (14, 'badge-blue'),
        (15, 'badge-purple'),   (16, 'badge-pink'),      (17, 'badge-brown')
),
remaining AS (
    SELECT id, ROW_NUMBER() OVER (ORDER BY id) - 1 AS rn
    FROM issue_types
    WHERE color = ''
)
UPDATE issue_types it
SET color = p.cls
FROM remaining r
JOIN palette p ON p.idx = r.rn % (SELECT COUNT(*) FROM palette)
WHERE it.id = r.id;
