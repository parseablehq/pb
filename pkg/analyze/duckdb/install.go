package duckdb

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"pb/pkg/common"
	"runtime"
	"strings"
)

func CheckAndInstallDuckDB() {
	// Check if DuckDB is already installed
	if _, err := exec.LookPath("duckdb"); err == nil {
		fmt.Println(common.Green + "DuckDB is already installed." + common.Reset)
		return
	}

	fmt.Println(common.Yellow + "DuckDB is not installed. Installing..." + common.Reset)

	// Define the download URL based on the OS
	var url, binaryName string
	if runtime.GOOS == "linux" {
		url = "https://github.com/duckdb/duckdb/releases/download/v1.1.3/duckdb_cli-linux-amd64.zip"
		binaryName = "duckdb"
	} else if runtime.GOOS == "darwin" {
		url = "https://github.com/duckdb/duckdb/releases/download/v1.1.3/duckdb_cli-osx-universal.zip"
		binaryName = "duckdb"
	} else {
		fmt.Println(common.Red + "Unsupported OS." + common.Reset)
		os.Exit(1)
	}

	// Download the DuckDB ZIP file
	zipFilePath := "/tmp/duckdb_cli.zip"
	err := downloadFile(zipFilePath, url)
	if err != nil {
		fmt.Printf(common.Red+"Failed to download DuckDB: %v\n"+common.Reset, err)
		os.Exit(1)
	}

	// Extract the ZIP file
	extractPath := "/tmp/duckdb_extracted"
	err = unzip(zipFilePath, extractPath)
	if err != nil {
		fmt.Printf(common.Red+"Failed to extract DuckDB: %v\n"+common.Reset, err)
		os.Exit(1)
	}

	// Install to the user’s bin directory
	userBinDir, _ := os.UserHomeDir()
	finalPath := filepath.Join(userBinDir, "bin", binaryName)

	// Ensure the directory exists
	if err := os.MkdirAll(filepath.Dir(finalPath), os.ModePerm); err != nil {
		fmt.Printf(common.Red+"Failed to create bin directory: %v\n"+common.Reset, err)
		os.Exit(1)
	}

	// Move the binary to ~/bin
	err = os.Rename(filepath.Join(extractPath, binaryName), finalPath)
	if err != nil {
		fmt.Printf(common.Red+"Failed to install DuckDB: %v\n"+common.Reset, err)
		os.Exit(1)
	}

	// Ensure the binary is executable
	if err := os.Chmod(finalPath, 0755); err != nil {
		fmt.Printf(common.Red+"Failed to set permissions on DuckDB: %v\n"+common.Reset, err)
		os.Exit(1)
	}

	// Add ~/bin to PATH automatically
	addToPath()

	fmt.Println(common.Green + "DuckDB successfully installed in " + finalPath + common.Reset)
}

// addToPath ensures ~/bin is in the user's PATH
func addToPath() {
	shellProfile := ""
	if _, exists := os.LookupEnv("ZSH_VERSION"); exists {
		shellProfile = filepath.Join(os.Getenv("HOME"), ".zshrc")
	} else {
		shellProfile = filepath.Join(os.Getenv("HOME"), ".bashrc")
	}

	// Check if ~/bin is already in the PATH
	data, err := os.ReadFile(shellProfile)
	if err == nil && !strings.Contains(string(data), "export PATH=$PATH:$HOME/bin") {
		// Append the export command
		f, err := os.OpenFile(shellProfile, os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			fmt.Printf(common.Red+"Failed to update PATH: %v\n"+common.Reset, err)
			return
		}
		defer f.Close()

		if _, err := f.WriteString("\nexport PATH=$PATH:$HOME/bin\n"); err != nil {
			fmt.Printf(common.Red+"Failed to write to shell profile: %v\n"+common.Reset, err)
		} else {
			fmt.Println(common.Green + "Updated PATH in " + shellProfile + common.Reset)
		}
	}
}

// downloadFile downloads a file from the given URL
func downloadFile(filepath string, url string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

// unzip extracts a ZIP file to the specified destination
func unzip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		extractedFilePath := filepath.Join(dest, f.Name)
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer rc.Close()

		// Create directories if needed
		if f.FileInfo().IsDir() {
			os.MkdirAll(extractedFilePath, os.ModePerm)
		} else {
			os.MkdirAll(filepath.Dir(extractedFilePath), os.ModePerm)
			outFile, err := os.Create(extractedFilePath)
			if err != nil {
				return err
			}
			defer outFile.Close()

			_, err = io.Copy(outFile, rc)
			if err != nil {
				return err
			}
		}
	}
	return nil
}
