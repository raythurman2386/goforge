package project

import "github.com/raythurman2386/goforge/internal/database/repositories"

// Alias the repository sentinel so in-memory stores can model a missing row
// without importing the concrete persistence package throughout tests.
var errRecordNotFound = repositories.ErrNotFound
