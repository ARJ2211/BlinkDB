### 1A decisions

    Value type: string

    Versioning: first Set creates version 1; every Set on the same key increments by 1 (even if value is the same)

    Timestamps: createdAt is set only on the first Set; updatedAt changes on every Set

    Get return: (Entry, bool) where bool = found

    Delete return: bool where true = a key was removed, false = key didn’t exist
