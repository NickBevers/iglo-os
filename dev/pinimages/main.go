// pinimages pins the images of the Iglo-OS stacks to the digest that their
// tag points to now, with the version from the image's own label. Run it
// before a release to move the stacks to the newest images, then test them:
//
//	go run ./dev/pinimages deploy/stacks/arr/compose.yaml
//
// A line like `image: lscr.io/linuxserver/radarr:latest` becomes
// `image: lscr.io/linuxserver/radarr:latest@sha256:… # 6.4.4.10123, pinned 2026-10-28`.
// An existing digest is replaced. Only the linux/amd64 image is read for the
// version; the digest is the one of the tag, which covers every platform.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

// imageLine finds `image: repo:tag` with an optional digest and comment.
var imageLine = regexp.MustCompile(`^(\s*image:\s*)([a-z0-9./_-]+):([A-Za-z0-9._-]+)(@sha256:[a-f0-9]{64})?(\s*#.*)?$`)

const (
	indexTypes    = "application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json"
	manifestTypes = "application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json"
)

func main() {
	flag.Parse()
	if flag.NArg() == 0 {
		log.Fatal("give the compose files to pin")
	}
	today := time.Now().Format("2006-01-02")
	for _, path := range flag.Args() {
		data, err := os.ReadFile(path)
		if err != nil {
			log.Fatal(err)
		}
		lines := strings.Split(string(data), "\n")
		for index, line := range lines {
			match := imageLine.FindStringSubmatch(line)
			if match == nil {
				continue
			}
			repo, tag := match[2], match[3]
			digest, version, err := resolve(repo, tag)
			if err != nil {
				log.Fatalf("%s: %s:%s: %v", path, repo, tag, err)
			}
			lines[index] = fmt.Sprintf("%s%s:%s@%s # %s, pinned %s", match[1], repo, tag, digest, version, today)
			log.Printf("%s:%s -> %s (%s)", repo, tag, digest[:19], version)
		}
		if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
			log.Fatal(err)
		}
	}
}

// registry is where a repository lives, and how to get a pull token for it.
func registry(repo string) (host, name string) {
	switch {
	case strings.HasPrefix(repo, "ghcr.io/"):
		return "ghcr.io", strings.TrimPrefix(repo, "ghcr.io/")
	case strings.HasPrefix(repo, "lscr.io/"):
		// LinuxServer's registry serves the images of ghcr.io/linuxserver.
		return "ghcr.io", strings.TrimPrefix(repo, "lscr.io/")
	case strings.HasPrefix(repo, "docker.io/"):
		return "registry-1.docker.io", strings.TrimPrefix(repo, "docker.io/")
	case strings.Count(repo, "/") == 1 && !strings.Contains(strings.Split(repo, "/")[0], "."):
		return "registry-1.docker.io", repo
	}
	log.Fatalf("unknown registry for %s", repo)
	return "", ""
}

func token(host, name string) (string, error) {
	url := "https://ghcr.io/token?scope=repository:" + name + ":pull"
	if host == "registry-1.docker.io" {
		url = "https://auth.docker.io/token?service=registry.docker.io&scope=repository:" + name + ":pull"
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := getJSON(url, "", "", &body); err != nil {
		return "", err
	}
	return body.Token, nil
}

// resolve returns the digest of repo:tag and the version label of its amd64 image.
func resolve(repo, tag string) (digest, version string, err error) {
	host, name := registry(repo)
	auth, err := token(host, name)
	if err != nil {
		return "", "", err
	}
	base := "https://" + host + "/v2/" + name

	request, _ := http.NewRequest(http.MethodHead, base+"/manifests/"+tag, nil)
	request.Header.Set("Authorization", "Bearer "+auth)
	request.Header.Set("Accept", indexTypes+", "+manifestTypes)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", "", err
	}
	response.Body.Close()
	digest = response.Header.Get("Docker-Content-Digest")
	if response.StatusCode != http.StatusOK || !strings.HasPrefix(digest, "sha256:") {
		return "", "", fmt.Errorf("manifest: %s", response.Status)
	}

	// The amd64 image of the index, then its config, for the labels.
	var index struct {
		Manifests []struct {
			Digest   string `json:"digest"`
			Platform struct {
				OS           string `json:"os"`
				Architecture string `json:"architecture"`
			} `json:"platform"`
		} `json:"manifests"`
		Config struct {
			Digest string `json:"digest"`
		} `json:"config"`
	}
	if err := getJSON(base+"/manifests/"+digest, auth, indexTypes+", "+manifestTypes, &index); err != nil {
		return "", "", err
	}
	config := index.Config.Digest
	for _, manifest := range index.Manifests {
		if manifest.Platform.OS == "linux" && manifest.Platform.Architecture == "amd64" {
			var image struct {
				Config struct {
					Digest string `json:"digest"`
				} `json:"config"`
			}
			if err := getJSON(base+"/manifests/"+manifest.Digest, auth, manifestTypes, &image); err != nil {
				return "", "", err
			}
			config = image.Config.Digest
		}
	}
	if config == "" {
		return "", "", fmt.Errorf("no linux/amd64 image")
	}
	var blob struct {
		Config struct {
			Labels map[string]string `json:"Labels"`
		} `json:"config"`
	}
	if err := getJSON(base+"/blobs/"+config, auth, "", &blob); err != nil {
		return "", "", err
	}
	version = blob.Config.Labels["org.opencontainers.image.version"]
	if version == "" {
		version = blob.Config.Labels["build_version"]
	}
	if version == "" {
		version = "version unknown"
	}
	return digest, strings.TrimSpace(version), nil
}

func getJSON(url, auth, accept string, out any) error {
	request, _ := http.NewRequest(http.MethodGet, url, nil)
	if auth != "" {
		request.Header.Set("Authorization", "Bearer "+auth)
	}
	if accept != "" {
		request.Header.Set("Accept", accept)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", url, response.Status)
	}
	return json.NewDecoder(response.Body).Decode(out)
}
