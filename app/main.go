package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

type TokenResponse struct {
	Token       string `json:"token"`
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	IssuedAt    string `json:"issued_at"`
}

type Manifest struct {
	SchemaVersion int    `json:"schemaVersion"`
	MediaType     string `json:"mediaType"`
	Config        struct {
		MediaType string `json:"mediaType"`
		Size      int    `json:"size"`
		Digest    string `json:"digest"`
	}
	Layers []struct {
		MediaType string `json:"mediaType"`
		Size      int    `json:"size"`
		Digest    string `json:"digest"`
	}
}

const (
	getTokenURL         = "https://auth.docker.io/token?service=registry.docker.io&scope=repository:library/%s:pull"
	getImageManifestURL = "https://registry.hub.docker.com/v2/library/%s/manifests/%s"
	pullDockerLayerURL  = "https://registry.hub.docker.com/v2/library/%s/blobs/%s"
)

func getAuthToken(imageName string) string {
	res, err := http.Get(fmt.Sprintf(getTokenURL, imageName))
	if err != nil {
		fmt.Printf("getAuthToken(): HTTP GET error ", err)
	}
	defer res.Body.Close()
	var docker_token TokenResponse
	body, err := io.ReadAll(res.Body)
	if err != nil {
		fmt.Printf("getAuthToken(): ioutil.ReadAll error ", err)
	}

	err = json.Unmarshal(body, &docker_token)
	if err != nil {
		fmt.Printf("getAuthToken(): json.Unmarshal error ", err)
	}
	return docker_token.Token
}

func getImageManifest(token, imageName string) Manifest {
	client := &http.Client{}
	req, err := http.NewRequest("GET", fmt.Sprintf(getImageManifestURL, imageName), nil)
	if err != nil {
		fmt.Printf("Err: %v", err)
		os.Exit(1)
	}
	req.Header.Add("Authorization", "Bearer "+token)
	req.Header.Add("Accept", "application/vnd.docker.distribution.manifest.v2+json")
	res, err := client.Do(req)
	if err != nil {
		fmt.Printf("Err: %v", err)
		os.Exit(1)
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		fmt.Printf("Err: %v", err)
		os.Exit(1)
	}
	var manifest Manifest
	err = json.Unmarshal(body, &manifest)
	if err != nil {
		fmt.Printf("Err: %v", err)
		os.Exit(1)
	}
	defer res.Body.Close()
	return manifest
}

func pullDockerLayer(imageName string, download_path string) {
	var token string = getAuthToken(imageName)
	var manifest Manifest = getImageManifest(token, imageName)
	client := &http.Client{}
	for _, layer := range manifest.Layers {
		req, err := http.NewRequest("GET", fmt.Sprintf(pullDockerLayerURL, imageName, layer.Digest), nil)
		if err != nil {
			fmt.Printf("Err: %v", err)
			os.Exit(1)
		}
		req.Header.Add("Authorization", "Bearer "+token)
		req.Header.Add("Accept", "application/vnd.docker.distribution.manifest.v2+json")
		res, err := client.Do(req)
		if err != nil {
			fmt.Printf("Err: %v", err)
			os.Exit(1)
		}
		defer res.Body.Close()

		layer_path := filepath.Join(download_path, "docker_layer.tar")
		file, err := os.Create(layer_path)
		if err != nil {
			fmt.Printf("Err: %v", err)
			os.Exit(1)
		}
		_, err = io.Copy(file, res.Body)
		if err != nil {
			fmt.Printf("Err: %v", err)
			os.Exit(1)
		}
		defer file.Close()

		cmd := exec.Command("tar", "-xf", layer_path, "-C", download_path)
		if err := cmd.Run(); err != nil {
			fmt.Printf("Err: %v", err)
			os.Exit(1)
		}
		if err = os.Remove(layer_path); err != nil {
			fmt.Printf("Err: %v", err)
			os.Exit(1)
		}
	}
}

func copyFile(destination string, source string) error {
	/*
		Use defer and Close() whenever we are working on files
		(i.e., using APIs such as os.Open or os.Create) as it kills the process associated with it.
		Otherwise we would encounter "text file busy" error.
	*/
	sourceFile, err := os.Open(source)
	if err != nil {
		return err
	}
	defer sourceFile.Close()
	sourceStat, err := sourceFile.Stat()
	if err != nil {
		return err
	}
	sourcePermission := sourceStat.Mode()
	destinationFile, err := os.Create(destination)
	if err != nil {
		return err
	}
	defer destinationFile.Close()
	_, err = io.Copy(destinationFile, sourceFile)
	if err != nil {
		return err
	}
	err = destinationFile.Chmod(sourcePermission)
	if err != nil {
		return err
	}
	return nil
}

func main() {
	var imageName string = os.Args[2]
	command := os.Args[3]
	args := os.Args[4:len(os.Args)]

	// Create root of executable command
	tempDir, err := os.MkdirTemp("", "sandbox")
	if err != nil {
		fmt.Printf("Err: %v", err)
	}

	defer os.RemoveAll(tempDir)

	pullDockerLayer(imageName, tempDir)

	// Copy the binary command to the new root
	command, err = exec.LookPath(command)
	if err != nil {
		fmt.Printf("Err: %v", err)
	}

	// enter the chroot
	if err := syscall.Chroot(tempDir); err != nil {
		fmt.Printf("Err: %v", err)
	}

	os.Mkdir("/dev", 0755)
	devNull, _ := os.Create("/dev/null")
	devNull.Close()

	cmd := exec.Command(command, args...)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Isolate process
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWPID | syscall.CLONE_NEWUTS,
	}
	err = cmd.Run()

	if err != nil {
		fmt.Printf("Err: %v", err)
		if exitError, ok := err.(*exec.ExitError); ok {
			os.Exit(exitError.ExitCode())
		}
	}
	os.Exit(0)
}
