package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/baldaworks/boyan"
	"github.com/baldaworks/boyan/app"
	bleveindex "github.com/baldaworks/boyan/index/bleve"
	badgerstore "github.com/baldaworks/boyan/store/badger"
	"github.com/spf13/cobra"
)

type migrationPhase string

const (
	migrationPhaseExported  migrationPhase = "exported"
	migrationPhaseImporting migrationPhase = "importing"
	migrationPhaseImported  migrationPhase = "imported"
	migrationPhaseVerified  migrationPhase = "verified"
	migrationPhaseRebuilt   migrationPhase = "rebuilding_projection"
	migrationPhaseComplete  migrationPhase = "completed"
)

type baldaMigrationReceipt struct {
	SchemaVersion   string         `json:"schema_version"`
	Command         string         `json:"command"`
	SourceDir       string         `json:"source_dir"`
	StateDir        string         `json:"state_dir"`
	RecordCount     uint64         `json:"record_count"`
	SHA256          string         `json:"sha256"`
	ImportedRecords uint64         `json:"imported_records"`
	ScopeCount      int            `json:"scope_count"`
	Phase           migrationPhase `json:"phase"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	CompletedAt     *time.Time     `json:"completed_at,omitempty"`
}

type resolvedPath struct {
	Abs    string
	Real   string
	Exists bool
}

type baldaMigrator struct {
	now              func() time.Time
	onImportProgress func(uint64) error
}

const migrateSourceFlag = "source"

func newMigrateCommand(configPath *string) *cobra.Command {
	command := &cobra.Command{
		Use:           "migrate",
		Short:         "Run Boyan data migrations.",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	command.AddCommand(newMigrateBaldaCommand(configPath))
	return command
}

func newMigrateBaldaCommand(configPath *string) *cobra.Command {
	var sourceDir string
	command := &cobra.Command{
		Use:   "balda",
		Short: "Import a Balda canonical store into standalone Boyan state.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadApplicationConfig(derefString(configPath), cmd.Flags())
			if err != nil {
				return err
			}
			if strings.TrimSpace(sourceDir) == "" {
				return formatUsageError("--%s is required", migrateSourceFlag)
			}
			return runBaldaMigration(cmd.Context(), sourceDir, cfg.StateDir, cmd.OutOrStdout())
		},
	}
	command.Flags().StringVar(&sourceDir, migrateSourceFlag, "", "Balda canonical session-memory directory")
	command.Flags().String(configFlagStateDir, "", "Boyan state root")
	return command
}

func runMigrate(ctx context.Context, args []string, stdout io.Writer) error {
	command := newMigrateCommand(nil)
	command.SetOut(stdout)
	command.SetErr(io.Discard)
	command.SetArgs(args)
	if ctx == nil {
		ctx = context.Background()
	}
	command.SetContext(ctx)
	return command.Execute()
}

func runBaldaMigration(ctx context.Context, sourceDir, stateDir string, stdout io.Writer) error {
	migrator := baldaMigrator{now: func() time.Time { return time.Now().UTC() }}
	if err := migrator.run(ctx, sourceDir, stateDir); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "boyan migrate balda completed: %s\n", filepath.Join(stateDir, baldaMigrationDirName, baldaMigrationReceiptName))
	return nil
}

func (m baldaMigrator) run(ctx context.Context, sourceDir, stateDir string) error {
	if ctx == nil {
		return fmt.Errorf("migration context is required")
	}
	source, err := resolveExistingDirectory(sourceDir)
	if err != nil {
		return fmt.Errorf("resolve source directory: %w", err)
	}
	state, err := resolveStateDirectory(stateDir)
	if err != nil {
		return fmt.Errorf("resolve state directory: %w", err)
	}
	if overlapsPath(source.Real, state.Abs) || overlapsPath(state.Abs, source.Real) {
		return fmt.Errorf("source and target state directories overlap")
	}
	if err := os.MkdirAll(state.Abs, 0o700); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	migrationDir := filepath.Join(state.Abs, baldaMigrationDirName)
	if err := os.MkdirAll(migrationDir, 0o700); err != nil {
		return fmt.Errorf("create migration directory: %w", err)
	}
	receiptPath := filepath.Join(migrationDir, baldaMigrationReceiptName)
	exportPath := filepath.Join(migrationDir, baldaMigrationExportName)
	receipt, err := loadBaldaMigrationReceipt(receiptPath)
	if err != nil {
		return fmt.Errorf("load migration receipt: %w", err)
	}
	if receipt == nil {
		if err := ensureStateDirEmptyExceptMigration(state.Abs); err != nil {
			return err
		}
	} else {
		if receipt.SchemaVersion != baldaMigrationReceiptSchema || receipt.Command != "boyan migrate balda" {
			return fmt.Errorf("migration receipt is invalid")
		}
		if receipt.SourceDir != source.Real || receipt.StateDir != state.Abs {
			return fmt.Errorf("migration receipt does not match this source or target")
		}
		if _, err := os.Stat(exportPath); err != nil {
			return fmt.Errorf("migration export artifact is missing: %w", err)
		}
	}

	sourceStore, err := badgerstore.OpenBadgerSessionMemoryStore(source.Real)
	if err != nil {
		return fmt.Errorf("open source canonical state: %w", err)
	}
	defer func() { _ = sourceStore.Close() }()
	if err := sourceStore.VerifyCanonicalIntegrity(ctx); err != nil {
		return fmt.Errorf("verify source canonical integrity: %w", err)
	}

	if receipt == nil {
		summary, err := exportLogicalSnapshot(ctx, sourceStore, exportPath)
		if err != nil {
			return err
		}
		now := m.currentTime()
		receipt = &baldaMigrationReceipt{
			SchemaVersion: baldaMigrationReceiptSchema,
			Command:       "boyan migrate balda",
			SourceDir:     source.Real,
			StateDir:      state.Abs,
			RecordCount:   summary.RecordCount,
			SHA256:        summary.SHA256,
			ScopeCount:    len(summary.Scopes),
			Phase:         migrationPhaseExported,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		if err := writeBaldaMigrationReceipt(receiptPath, *receipt); err != nil {
			return err
		}
	}

	if receipt.Phase == migrationPhaseComplete {
		if err := verifyCompletedMigration(ctx, state.Abs, *receipt); err != nil {
			return err
		}
		return nil
	}

	targetCanonicalDir := filepath.Join(state.Abs, boyanCanonicalDirName)
	targetStore, err := badgerstore.OpenBadgerSessionMemoryStore(targetCanonicalDir)
	if err != nil {
		return fmt.Errorf("open target canonical state: %w", err)
	}
	defer func() { _ = targetStore.Close() }()

	exportFile, err := os.Open(exportPath)
	if err != nil {
		return fmt.Errorf("open migration export artifact: %w", err)
	}
	summary, err := targetStore.ImportCanonicalLogicalWithOptions(ctx, exportFile, badgerstore.CanonicalLogicalImportOptions{
		BatchSize: 256,
		Progress: func(progress badgerstore.CanonicalLogicalImportProgress) error {
			receipt.Phase = migrationPhaseImporting
			receipt.ImportedRecords = progress.ImportedRecords
			receipt.UpdatedAt = m.currentTime()
			if err := writeBaldaMigrationReceipt(receiptPath, *receipt); err != nil {
				return err
			}
			if m.onImportProgress != nil {
				return m.onImportProgress(progress.ImportedRecords)
			}
			return nil
		},
	})
	_ = exportFile.Close()
	if err != nil {
		return err
	}
	if summary.RecordCount != receipt.RecordCount || summary.SHA256 != receipt.SHA256 {
		return fmt.Errorf("logical import summary does not match exported snapshot")
	}
	receipt.Phase = migrationPhaseImported
	receipt.ImportedRecords = summary.RecordCount
	receipt.ScopeCount = len(summary.Scopes)
	receipt.UpdatedAt = m.currentTime()
	if err := writeBaldaMigrationReceipt(receiptPath, *receipt); err != nil {
		return err
	}

	if err := targetStore.VerifyCanonicalIntegrity(ctx); err != nil {
		return fmt.Errorf("verify target canonical integrity: %w", err)
	}
	targetSummary, err := summarizeCanonicalStore(ctx, targetStore, migrationDir)
	if err != nil {
		return err
	}
	if targetSummary.RecordCount != receipt.RecordCount || targetSummary.SHA256 != receipt.SHA256 {
		return fmt.Errorf("target logical verification does not match exported snapshot")
	}
	receipt.Phase = migrationPhaseVerified
	receipt.ScopeCount = len(targetSummary.Scopes)
	receipt.UpdatedAt = m.currentTime()
	if err := writeBaldaMigrationReceipt(receiptPath, *receipt); err != nil {
		return err
	}

	receipt.Phase = migrationPhaseRebuilt
	receipt.UpdatedAt = m.currentTime()
	if err := writeBaldaMigrationReceipt(receiptPath, *receipt); err != nil {
		return err
	}
	if err := rebuildBleveProjection(ctx, targetStore, filepath.Join(state.Abs, boyanProjectionDirName), targetSummary.Scopes); err != nil {
		return err
	}

	completedAt := m.currentTime()
	receipt.Phase = migrationPhaseComplete
	receipt.ImportedRecords = receipt.RecordCount
	receipt.UpdatedAt = completedAt
	receipt.CompletedAt = &completedAt
	return writeBaldaMigrationReceipt(receiptPath, *receipt)
}

func exportLogicalSnapshot(ctx context.Context, store *badgerstore.BadgerSessionMemoryStore, exportPath string) (badgerstore.CanonicalLogicalExportSummary, error) {
	tempFile, err := os.CreateTemp(filepath.Dir(exportPath), "balda-export-*.jsonl")
	if err != nil {
		return badgerstore.CanonicalLogicalExportSummary{}, fmt.Errorf("create logical export artifact: %w", err)
	}
	tempPath := tempFile.Name()
	defer func() { _ = os.Remove(tempPath) }()
	if err := store.ExportCanonicalLogical(ctx, tempFile); err != nil {
		_ = tempFile.Close()
		return badgerstore.CanonicalLogicalExportSummary{}, fmt.Errorf("export logical snapshot: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return badgerstore.CanonicalLogicalExportSummary{}, fmt.Errorf("close logical export artifact: %w", err)
	}
	if err := os.Rename(tempPath, exportPath); err != nil {
		return badgerstore.CanonicalLogicalExportSummary{}, fmt.Errorf("activate logical export artifact: %w", err)
	}
	return summarizeLogicalExportFile(ctx, exportPath)
}

func summarizeLogicalExportFile(ctx context.Context, path string) (badgerstore.CanonicalLogicalExportSummary, error) {
	file, err := os.Open(path)
	if err != nil {
		return badgerstore.CanonicalLogicalExportSummary{}, fmt.Errorf("open logical export artifact: %w", err)
	}
	defer func() { _ = file.Close() }()
	summary, err := badgerstore.ScanCanonicalLogicalExport(ctx, file, 256, nil)
	if err != nil {
		return badgerstore.CanonicalLogicalExportSummary{}, fmt.Errorf("scan logical export artifact: %w", err)
	}
	return summary, nil
}

func summarizeCanonicalStore(ctx context.Context, store *badgerstore.BadgerSessionMemoryStore, directory string) (badgerstore.CanonicalLogicalExportSummary, error) {
	tempFile, err := os.CreateTemp(directory, "boyan-verify-*.jsonl")
	if err != nil {
		return badgerstore.CanonicalLogicalExportSummary{}, fmt.Errorf("create verification export: %w", err)
	}
	tempPath := tempFile.Name()
	defer func() {
		_ = tempFile.Close()
		_ = os.Remove(tempPath)
	}()
	if err := store.ExportCanonicalLogical(ctx, tempFile); err != nil {
		return badgerstore.CanonicalLogicalExportSummary{}, fmt.Errorf("export target logical snapshot: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return badgerstore.CanonicalLogicalExportSummary{}, fmt.Errorf("close target logical snapshot: %w", err)
	}
	return summarizeLogicalExportFile(ctx, tempPath)
}

func rebuildBleveProjection(ctx context.Context, store *badgerstore.BadgerSessionMemoryStore, projectionRoot string, scopes []boyan.Scope) error {
	reader, err := badgerstore.NewCanonicalReader(store)
	if err != nil {
		return fmt.Errorf("create canonical reader: %w", err)
	}
	projection, err := bleveindex.Open(projectionRoot)
	if err != nil {
		return fmt.Errorf("open bleve projection: %w", err)
	}
	applier, err := bleveindex.NewBleveCanonicalApplier(projection, reader)
	if err != nil {
		_ = projection.Close()
		return fmt.Errorf("create bleve projection applier: %w", err)
	}
	defer func() { _ = applier.Close() }()
	runtime, err := app.NewProjectionRuntime(store, store, applier, boyanProjectionID, 0)
	if err != nil {
		return fmt.Errorf("create projection runtime: %w", err)
	}
	defer func() { _ = runtime.Close(ctx) }()
	for _, scope := range scopes {
		if err := runtime.Sync(ctx, scope); err != nil {
			return fmt.Errorf("rebuild bleve projection for scope %s/%s: %w", scope.Kind, scope.Key, err)
		}
	}
	return nil
}

func verifyCompletedMigration(ctx context.Context, stateDir string, receipt baldaMigrationReceipt) error {
	targetStore, err := badgerstore.OpenBadgerSessionMemoryStore(filepath.Join(stateDir, boyanCanonicalDirName))
	if err != nil {
		return fmt.Errorf("open completed target canonical state: %w", err)
	}
	defer func() { _ = targetStore.Close() }()
	if err := targetStore.VerifyCanonicalIntegrity(ctx); err != nil {
		return fmt.Errorf("verify completed target canonical integrity: %w", err)
	}
	summary, err := summarizeCanonicalStore(ctx, targetStore, filepath.Join(stateDir, baldaMigrationDirName))
	if err != nil {
		return err
	}
	if summary.RecordCount != receipt.RecordCount || summary.SHA256 != receipt.SHA256 {
		return fmt.Errorf("completed migration does not match its receipt")
	}
	return nil
}

func loadBaldaMigrationReceipt(path string) (*baldaMigrationReceipt, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var receipt baldaMigrationReceipt
	if err := json.Unmarshal(data, &receipt); err != nil {
		return nil, err
	}
	return &receipt, nil
}

func writeBaldaMigrationReceipt(path string, receipt baldaMigrationReceipt) error {
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return fmt.Errorf("encode migration receipt: %w", err)
	}
	data = append(data, '\n')
	tempFile, err := os.CreateTemp(filepath.Dir(path), "balda-receipt-*.json")
	if err != nil {
		return fmt.Errorf("create migration receipt: %w", err)
	}
	tempPath := tempFile.Name()
	defer func() { _ = os.Remove(tempPath) }()
	if _, err := tempFile.Write(data); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("write migration receipt: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close migration receipt: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("activate migration receipt: %w", err)
	}
	return nil
}

func ensureStateDirEmptyExceptMigration(stateDir string) error {
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		return fmt.Errorf("read target state directory: %w", err)
	}
	for _, entry := range entries {
		if entry.Name() == baldaMigrationDirName {
			migrationEntries, readErr := os.ReadDir(filepath.Join(stateDir, baldaMigrationDirName))
			if readErr != nil {
				return fmt.Errorf("read migration directory: %w", readErr)
			}
			if len(migrationEntries) == 0 {
				continue
			}
		}
		return fmt.Errorf("target state directory is not empty and has no matching migration receipt")
	}
	return nil
}

func resolveExistingDirectory(path string) (resolvedPath, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return resolvedPath{}, fmt.Errorf("directory is required")
	}
	abs, err := filepath.Abs(trimmed)
	if err != nil {
		return resolvedPath{}, err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return resolvedPath{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return resolvedPath{}, fmt.Errorf("symlink paths are not allowed")
	}
	if !info.IsDir() {
		return resolvedPath{}, fmt.Errorf("path is not a directory")
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return resolvedPath{}, err
	}
	if filepath.Clean(real) != filepath.Clean(abs) {
		return resolvedPath{}, fmt.Errorf("path aliasing is not allowed")
	}
	return resolvedPath{Abs: filepath.Clean(abs), Real: filepath.Clean(real), Exists: true}, nil
}

func resolveStateDirectory(path string) (resolvedPath, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return resolvedPath{}, fmt.Errorf("state directory is required")
	}
	abs, err := filepath.Abs(trimmed)
	if err != nil {
		return resolvedPath{}, err
	}
	abs = filepath.Clean(abs)
	info, err := os.Lstat(abs)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return resolvedPath{}, fmt.Errorf("symlink paths are not allowed")
		}
		if !info.IsDir() {
			return resolvedPath{}, fmt.Errorf("path is not a directory")
		}
		real, evalErr := filepath.EvalSymlinks(abs)
		if evalErr != nil {
			return resolvedPath{}, evalErr
		}
		if filepath.Clean(real) != abs {
			return resolvedPath{}, fmt.Errorf("path aliasing is not allowed")
		}
		return resolvedPath{Abs: abs, Real: filepath.Clean(real), Exists: true}, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return resolvedPath{}, err
	}
	parent, err := firstExistingParent(abs)
	if err != nil {
		return resolvedPath{}, err
	}
	realParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return resolvedPath{}, err
	}
	if filepath.Clean(realParent) != filepath.Clean(parent) {
		return resolvedPath{}, fmt.Errorf("path aliasing is not allowed")
	}
	return resolvedPath{Abs: abs, Real: abs, Exists: false}, nil
}

func firstExistingParent(path string) (string, error) {
	current := filepath.Clean(path)
	for {
		info, err := os.Lstat(current)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return "", fmt.Errorf("symlink paths are not allowed")
			}
			if !info.IsDir() {
				return "", fmt.Errorf("existing parent is not a directory")
			}
			return current, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		next := filepath.Dir(current)
		if next == current {
			return "", fmt.Errorf("no existing parent directory found")
		}
		current = next
	}
}

func overlapsPath(root, target string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(target))
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && rel != "..")
}

func (m baldaMigrator) currentTime() time.Time {
	if m.now == nil {
		return time.Now().UTC()
	}
	return m.now().UTC()
}
