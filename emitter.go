package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	log "github.com/sirupsen/logrus"
)

// marshals the graph to JSON and writes it atomically to a timestamped file
// in the specified output directory.
func EmitGraph(graph Graph, outputDir string) error {
	err := os.MkdirAll(outputDir, 0755)
	if err != nil {
		return fmt.Errorf("failed to create output directory %s: %w", outputDir, err)
	}

	jsonData, err := json.MarshalIndent(graph, "", "  ") // Use MarshalIndent for readability
	if err != nil {
		return fmt.Errorf("failed to marshal graph to JSON: %w", err)
	}

	tempFile, err := os.CreateTemp(outputDir, "graph-*.json.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temporary file: %w", err)
	}
	defer func() {
		if tempFile != nil {
			_ = tempFile.Close()
			_ = os.Remove(tempFile.Name())
		}
	}()

	_, err = tempFile.Write(jsonData)
	if err != nil {
		return fmt.Errorf("failed to write to temporary file %s: %w", tempFile.Name(), err)
	}
	err = tempFile.Sync()
	if err != nil {
		return fmt.Errorf("failed to sync temporary file %s: %w", tempFile.Name(), err)
	}
	err = tempFile.Close()
	if err != nil {
		return fmt.Errorf("failed to close temporary file %s: %w", tempFile.Name(), err)
	}

	timestamp := time.Now().Format("20060102-150405")
	finalFilename := filepath.Join(outputDir, fmt.Sprintf("graph-%s.json", timestamp))

	err = os.Rename(tempFile.Name(), finalFilename)
	if err != nil {
		return fmt.Errorf("failed to rename temporary file %s to %s: %w", tempFile.Name(), finalFilename, err)
	}

	tempFile = nil
	log.Infof("Successfully emitted graph revision %d to %s", graph.GraphRevision, finalFilename)
	return nil
}
