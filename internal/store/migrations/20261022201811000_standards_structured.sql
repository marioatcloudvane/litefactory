-- A standard becomes one structured MUST rule in exactly one category, applied
-- by named agent roles. The table is altered in place so retained exception
-- policies and proposals keep their references. Existing rows keep their text
-- as rule and description and their first category; the fields they never had
-- stay empty and fail verification until the standard is replaced.
ALTER TABLE standards_standards RENAME COLUMN title TO rule;
ALTER TABLE standards_standards RENAME COLUMN body TO description;
ALTER TABLE standards_standards ADD COLUMN category_id TEXT NOT NULL DEFAULT '';
ALTER TABLE standards_standards ADD COLUMN evidence TEXT NOT NULL DEFAULT '';
ALTER TABLE standards_standards ADD COLUMN scope TEXT NOT NULL DEFAULT '';
ALTER TABLE standards_standards ADD COLUMN check_kind TEXT NOT NULL DEFAULT '';
DROP TRIGGER standards_standards_immutable_update;
UPDATE standards_standards SET category_id=COALESCE((SELECT MIN(m.category_id) FROM standards_memberships m
 WHERE m.revision_id=standards_standards.revision_id AND m.standard_id=standards_standards.id),'');
CREATE TRIGGER standards_standards_immutable_update BEFORE UPDATE ON standards_standards BEGIN SELECT RAISE(ABORT, 'immutable standards record'); END;
DROP TABLE standards_memberships;
-- An added column cannot carry the foreign key, so a trigger keeps the one
-- category of every new standard inside its own revision.
CREATE TRIGGER standards_standards_category BEFORE INSERT ON standards_standards
WHEN NOT EXISTS(SELECT 1 FROM standards_categories WHERE revision_id=NEW.revision_id AND id=NEW.category_id)
BEGIN SELECT RAISE(ABORT, 'standard category is not in its revision'); END;
CREATE TABLE standards_applies_to (
 revision_id INTEGER NOT NULL, standard_id TEXT NOT NULL, role TEXT NOT NULL,
 PRIMARY KEY(revision_id,standard_id,role),
 FOREIGN KEY(revision_id,standard_id) REFERENCES standards_standards(revision_id,id)
);
CREATE TRIGGER standards_applies_to_immutable_update BEFORE UPDATE ON standards_applies_to BEGIN SELECT RAISE(ABORT, 'immutable standards record'); END;
CREATE TRIGGER standards_applies_to_immutable_delete BEFORE DELETE ON standards_applies_to BEGIN SELECT RAISE(ABORT, 'immutable standards record'); END;
CREATE TRIGGER standards_applies_to_sealed BEFORE INSERT ON standards_applies_to
WHEN EXISTS(SELECT 1 FROM standards_publications WHERE revision_id=NEW.revision_id)
BEGIN SELECT RAISE(ABORT, 'published standards snapshot is sealed'); END;
