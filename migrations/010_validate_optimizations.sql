-- Validate after migration 009 releases its exclusive table lock.
ALTER TABLE cully_entries VALIDATE CONSTRAINT cully_entries_entry_type_check;
ALTER TABLE cully_entries VALIDATE CONSTRAINT cully_entries_optimization_check;
