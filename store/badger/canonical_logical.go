package badger

import "github.com/baldaworks/boyan/store/archive"

const (
	canonicalLogicalExportSchema = "boyan-logical-export/v1"
	canonicalLogicalHeaderKind   = "header"
	canonicalLogicalRecordKind   = "record"
	canonicalLogicalFooterKind   = "footer"
	maxCanonicalLogicalLineBytes = 4 * 1024 * 1024
	defaultCanonicalImportBatch  = 256
)

// CanonicalLogicalExportHeader is retained until the Badger package retirement (TASK-020).
type CanonicalLogicalExportHeader = archive.CanonicalLogicalExportHeader

// CanonicalLogicalExportRecord is retained until the Badger package retirement (TASK-020).
type CanonicalLogicalExportRecord = archive.CanonicalLogicalExportRecord

// CanonicalLogicalExportFooter is retained until the Badger package retirement (TASK-020).
type CanonicalLogicalExportFooter = archive.CanonicalLogicalExportFooter

// CanonicalLogicalExportSummary is retained until the Badger package retirement (TASK-020).
type CanonicalLogicalExportSummary = archive.CanonicalLogicalExportSummary

// CanonicalLogicalImportOptions is retained until the Badger package retirement (TASK-020).
type CanonicalLogicalImportOptions = archive.CanonicalLogicalImportOptions

// CanonicalLogicalImportProgress is retained until the Badger package retirement (TASK-020).
type CanonicalLogicalImportProgress = archive.CanonicalLogicalImportProgress
