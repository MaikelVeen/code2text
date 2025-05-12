package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"
)

var (
	outputFile     string
	sizeThreshold  float64 // in MB
	extensionsStr  string
	excludeDirsStr string
)

var logger *slog.Logger

var codeFileExtensions = map[string]struct{}{
	".go": {}, ".py": {}, ".js": {}, ".ts": {}, ".java": {}, ".c": {}, ".cpp": {}, ".h": {}, ".hpp": {},
	".rs": {}, ".html": {}, ".css": {}, ".scss": {}, ".less": {}, ".json": {}, ".xml": {}, ".yaml": {}, ".yml": {},
	".md": {}, ".sh": {}, ".bash": {}, ".zsh": {}, ".rb": {}, ".php": {}, ".swift": {}, ".kt": {}, ".kts": {},
	".gradle": {}, ".pl": {}, ".pm": {}, ".lua": {}, ".sql": {}, ".r": {}, ".dart": {}, ".pas": {}, ".dfm": {},
	".cs": {}, ".fs": {}, ".vb": {}, ".vbs": {}, ".scala": {}, ".clj": {}, ".cljs": {}, ".edn": {},
	".erl": {}, ".hrl": {}, ".ex": {}, ".exs": {}, ".elm": {}, ".hs": {}, ".lhs": {}, ".feature": {},
	".tf": {}, ".tfvars": {}, ".hcl": {}, ".ini": {}, ".toml": {}, ".cfg": {}, ".conf": {}, ".properties": {},
	".dockerfile": {}, "Dockerfile": {}, "Makefile": {}, ".mod": {}, ".sum": {}, ".csproj": {}, ".sln": {},
	".yaml-tml": {}, ".json-tml": {}, ".xhtml": {}, ".phtml": {}, ".tpl": {}, ".env": {}, ".example": {},
	".graphql": {}, ".gql": {}, ".vue": {}, ".svelte": {}, ".jsx": {}, ".tsx": {},
}

var defaultExcludeDirs = map[string]struct{}{
	".git":             {},
	"node_modules":     {},
	"vendor":           {},
	".vscode":          {},
	".idea":            {},
	"__pycache__":      {},
	"build":            {},
	"dist":             {},
	"target":           {},
	"bin":              {},
	"obj":              {},
	"out":              {},
	".DS_Store":        {},
	".svn":             {},
	".hg":              {},
	"CVS":              {},
	".cache":           {},
	".pytest_cache":    {},
	".mypy_cache":      {},
	".tox":             {},
	".next":            {},
	".nuxt":            {},
	".svelte-kit":      {},
	"coverage":         {},
	"site":             {},
	"public":           {},
	"tmp":              {},
	"temp":             {},
	"logs":             {},
	"log":              {},
	"assets":           {},
	"static":           {},
	"migrations":       {},
	".terraform":       {},
	".serverless":      {},
	".venv":            {},
	"venv":             {},
	"env":              {},
	".env":             {},
	"jspm_packages":    {},
	"bower_components": {},
	"web_modules":      {},
}

var rootCmd = &cobra.Command{
	Use:   "code2txt",
	Short: "Concatenates code files into a single text file.",
	Long: `code2txt scans the current directory and its subdirectories,
applying filters for file type, size, and binary content,
then concatenates the results into a single text file.

Examples:
  code2txt
  code2txt -o project_src.txt
  code2txt -t 1 -extensions .config,.script
  code2txt -exclude-dirs test_data,temp_files`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return performCodeConcatenation()
	},
}

func init() {
	rootCmd.Flags().StringVarP(&outputFile, "output", "o", "code_output.txt", "Specify output file path")
	rootCmd.Flags().Float64VarP(&sizeThreshold, "threshold", "t", 0.5, "Set file size threshold in MB (0 or negative to disable)")
	rootCmd.Flags().StringVar(&extensionsStr, "extensions", "", "Comma-separated list of additional code file extensions to include (e.g., .txt,.log)")
	rootCmd.Flags().StringVar(&excludeDirsStr, "exclude-dirs", "", "Comma-separated list of directories to exclude (e.g., my_build,custom_assets)")

	logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
}

func performCodeConcatenation() error {
	currentCodeFileExtensions := make(map[string]struct{})
	for k, v := range codeFileExtensions {
		currentCodeFileExtensions[k] = v
	}
	if extensionsStr != "" {
		exts := strings.Split(extensionsStr, ",")
		for _, ext := range exts {
			trimmedExt := strings.TrimSpace(ext)
			if trimmedExt == "" {
				continue
			}
			if !strings.HasPrefix(trimmedExt, ".") && !strings.Contains(trimmedExt, "/") {
				if _, exists := currentCodeFileExtensions[trimmedExt]; !exists {
					trimmedExt = "." + trimmedExt
				}
			}
			currentCodeFileExtensions[trimmedExt] = struct{}{}
		}
	}

	finalExcludeDirs := make(map[string]struct{})
	for k, v := range defaultExcludeDirs {
		finalExcludeDirs[k] = v
	}
	if excludeDirsStr != "" {
		dirs := strings.Split(excludeDirsStr, ",")
		for _, dir := range dirs {
			trimmedDir := strings.TrimSpace(dir)
			if trimmedDir != "" {
				finalExcludeDirs[trimmedDir] = struct{}{}
			}
		}
	}

	startDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("error getting current directory: %w", err)
	}

	absOutputFile, err := filepath.Abs(outputFile)
	if err != nil {
		return fmt.Errorf("error resolving output file path: %w", err)
	}

	var contentBuilder strings.Builder
	var processedFilesCount int
	var skippedFilesCount int

	thresholdBytes := int64(sizeThreshold * 1024 * 1024)
	if sizeThreshold <= 0 {
		thresholdBytes = -1
	}

	walkErr := filepath.WalkDir(startDir, func(path string, d fs.DirEntry, errInWalk error) error {
		if errInWalk != nil {
			logger.Warn("Error accessing path, skipping", "path", path, "error", errInWalk)
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		absPath, err := filepath.Abs(path)
		if err != nil {
			logger.Warn("Could not get absolute path, skipping", "path", path, "error", err)
			skippedFilesCount++
			return nil
		}

		if absPath == absOutputFile {
			skippedFilesCount++
			return nil
		}

		if d.IsDir() {
			dirName := d.Name()
			if _, shouldExclude := finalExcludeDirs[dirName]; shouldExclude {
				return filepath.SkipDir
			}
			return nil
		}

		fileInfo, err := d.Info()
		if err != nil {
			logger.Warn("Error getting file info, skipping", "path", path, "error", err)
			skippedFilesCount++
			return nil
		}

		isCode := false
		fileName := d.Name()
		fileExt := filepath.Ext(fileName)

		if _, ok := currentCodeFileExtensions[fileName]; ok {
			isCode = true
		} else if fileExt != "" {
			if _, ok := currentCodeFileExtensions[fileExt]; ok {
				isCode = true
			}
		}

		if !isCode {
			skippedFilesCount++
			return nil
		}

		if thresholdBytes > 0 && fileInfo.Size() > thresholdBytes {
			skippedFilesCount++
			return nil
		}

		isBin, binCheckErr := isBinary(path)
		if binCheckErr != nil {
			logger.Warn("Could not check if file is binary, skipping", "path", path, "error", binCheckErr)
			skippedFilesCount++
			return nil
		}
		if isBin {
			skippedFilesCount++
			return nil
		}

		fileContent, err := os.ReadFile(path)
		if err != nil {
			logger.Warn("Error reading file, skipping", "path", path, "error", err)
			skippedFilesCount++
			return nil
		}

		relativePath, _ := filepath.Rel(startDir, path)

		contentBuilder.WriteString(fmt.Sprintf("\n%s\n", strings.Repeat("=", 80)))
		contentBuilder.WriteString(fmt.Sprintf("File: %s\n", relativePath))
		contentBuilder.WriteString(fmt.Sprintf("%s\n\n", strings.Repeat("=", 80)))
		contentBuilder.Write(fileContent)
		contentBuilder.WriteString("\n")

		processedFilesCount++
		fmt.Fprintf(os.Stdout, "\rProcessed: %d, Skipped: %d", processedFilesCount, skippedFilesCount)
		return nil
	})

	fmt.Fprintln(os.Stdout)

	if walkErr != nil {
		logger.Warn("Error encountered during directory walk", "error", walkErr)
	}

	if contentBuilder.Len() == 0 {
		logger.Info("No content was generated.")
		if processedFilesCount == 0 && skippedFilesCount > 0 {
			logger.Info("File processing summary", "processed", 0, "skipped", skippedFilesCount)
			logger.Info("Try adjusting filters or checking file permissions.")
		}
		return nil
	}

	outFile, err := os.Create(absOutputFile)
	if err != nil {
		return fmt.Errorf("error creating output file %q: %w", absOutputFile, err)
	}
	defer outFile.Close()

	writer := bufio.NewWriter(outFile)
	_, err = writer.WriteString(contentBuilder.String())
	if err != nil {
		return fmt.Errorf("error writing to output file: %w", err)
	}
	writer.Flush()

	logger.Info("Processing complete", "processed", processedFilesCount, "skipped", skippedFilesCount)
	logger.Info("Output saved", "path", absOutputFile)
	return nil
}

func isBinary(filePath string) (bool, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return false, fmt.Errorf("opening file: %w", err)
	}
	defer file.Close()

	buffer := make([]byte, 1024)
	n, err := file.Read(buffer)
	if err != nil && err.Error() != "EOF" {
		return false, fmt.Errorf("reading file: %w", err)
	}

	if n == 0 {
		return false, nil
	}
	actualBuffer := buffer[:n]

	if bytes.Contains(actualBuffer, []byte{0}) {
		return true, nil
	}

	if !utf8.Valid(actualBuffer) {
		suspiciousChars := 0
		for _, b := range actualBuffer {
			if (b < 32 && b != '\t' && b != '\n' && b != '\r') || b > 127 {
				suspiciousChars++
			}
		}
		if n > 0 && (float64(suspiciousChars)/float64(n) > 0.30) {
			return true, nil
		}
	}
	return false, nil
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
