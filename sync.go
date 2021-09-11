package main

import (
	"context"
	"crypto/md5"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/bradfitz/slice"
	"golang.org/x/oauth2/google"
	"golang.org/x/oauth2/jwt"
	drive "google.golang.org/api/drive/v3"
	option "google.golang.org/api/option"
)

type remoteFile struct {
	File   *drive.File
	Parent *drive.File
}

type syncedFile struct {
	File    *drive.File
	Path    string
	ModTime time.Time
}

type configuration struct {
	RootID string `json:"root_id"`
	Title  string `json:"title"`
}

//go:embed config.json
var googleServiceAccountConfiguration []byte

var appConf configuration
var apiConf *jwt.Config
var client *http.Client
var service *drive.Service

func synchronize() {
	setProgress(0.00, "Initialization...", false)
	err := json.Unmarshal(googleServiceAccountConfiguration, &appConf)
	if err != nil {
		setProgress(1, err.Error(), false)
		return
	}
	setProgress(0.00, "Initialization... OK", true)
	setProgress(0.01, "Connection...", false)
	apiConf, err = google.JWTConfigFromJSON(googleServiceAccountConfiguration, "https://www.googleapis.com/auth/drive")
	if err != nil {
		setProgress(1, err.Error(), false)
		return
	}

	client = apiConf.Client(context.Background())
	service, err = drive.NewService(context.Background(), option.WithHTTPClient(client))
	if err != nil {
		setProgress(1, err.Error(), false)
		return
	}
	// ...
	remoteFiles := map[string]*remoteFile{}
	files := []syncedFile{}

	setProgress(0.01, "Connection... OK", true)
	setProgress(0.02, "Remote index...", false)
	pageToken := ""
	for {
		q := service.Files.List().Fields("nextPageToken, files/*")
		// If we have a pageToken set, apply it to the query
		if pageToken != "" {
			q = q.PageToken(pageToken)
		}
		r, err := q.Do()
		if err != nil {
			setProgress(1, err.Error(), false)
			return
		}
		for _, file := range r.Files {
			remoteFiles[file.Id] = &remoteFile{
				Parent: nil,
				File:   file,
			}
			//setProgress(0.02, fmt.Sprintf("Indexing remote files... %s", file.Name), true)
		}
		pageToken = r.NextPageToken
		if pageToken == "" {
			break
		}
	}
	setProgress(0.02, "Remote index... OK", true)
	processName := filepath.Base(os.Args[0])

	setProgress(0.03, "File tree...", false)
	rootID := appConf.RootID
	for _, remoteFile := range remoteFiles {
		if len(remoteFile.File.Parents) == 0 {
			if rootID == "" {
				rootID = remoteFile.File.Id
			}
		} else {
			for _, parentID := range remoteFile.File.Parents {
				parent := remoteFiles[parentID]
				remoteFile.Parent = parent.File
			}
		}
	}
	setProgress(0.03, "File tree... OK", true)
	setProgress(0.04, "Comparison...", false)
	layout := "2006-01-02T15:04:05.000Z"
	loadedSize := uint64(0)
	loadedCount := 0
	totalSize := int64(0)
	totalCount := 0
	for _, remoteFile := range remoteFiles {
		if remoteFile.File.MimeType == "application/vnd.google-apps.folder" || remoteFile.File.Name == processName || remoteFile.File.Name == "FOnlineUpdater.cfg" {
			continue
		}
		pathParts := []string{}
		scope := remoteFile
		for (*scope).Parent != nil {
			pathParts = append([]string{(*scope).File.Name}, pathParts...)
			scope = remoteFiles[(*scope).Parent.Id]
		}
		filePath := filepath.Join(pathParts...)
		if filePath == "" {
			continue
		}
		fileSize, fileModTime, fileError := getFileStats(filePath)
		remoteLastModified, err := time.Parse(layout, remoteFile.File.ModifiedTime)
		if err != nil {
			setProgress(1, err.Error(), false)
			return
		}
		shouldDownload := fileError != nil || fileSize == 0 || (remoteLastModified.After(fileModTime))
		if !shouldDownload {
			fileMD5 := getFileMd5(filePath)
			shouldDownload = fileMD5 == "" || fileMD5 != remoteFile.File.Md5Checksum
		}
		if shouldDownload {
			files = append(files, syncedFile{
				File:    remoteFile.File,
				Path:    filePath,
				ModTime: remoteLastModified,
			})
			totalSize += remoteFile.File.Size
			totalCount++
		}
	}
	// ...
	slice.Sort(files[:], func(i, j int) bool {
		return files[i].File.Size > files[j].File.Size
	})
	interval := time.Millisecond * 500
	wg := sync.WaitGroup{}
	setProgress(0.04, "Comparison... OK", true)
	setProgress(0.05, fmt.Sprintf("Synchronization... %d/%d", 0, totalCount), false)
	for _, sFile := range files {
		time.Sleep(interval)
		wg.Add(1)
		// @todo: make it more memory-safe?
		go func(realPath string, tmpPath string, id string, mod time.Time) {
			t1 := time.Now()
			dir := filepath.Dir(realPath)
			os.MkdirAll(dir, os.ModePerm)
			resp, err := service.Files.Get(id).Download()
			if err != nil {
				setProgress(1, err.Error(), false)
				return
			}
			out, err := os.Create(tmpPath)
			if err != nil {
				resp.Body.Close()
				setProgress(1, err.Error(), false)
				return
			}
			prevSize := uint64(0)
			counter := &WriteCounter{
				Logger: func(n uint64) {
					loadedSize += n - prevSize
					prevSize = n
					setProgress(float64(loadedSize)/float64(totalSize)*0.95+0.05, fmt.Sprintf("Synchronization... %d/%d", loadedCount, totalCount), true)
				},
			}
			if _, err = io.Copy(out, io.TeeReader(resp.Body, counter)); err != nil {
				out.Close()
				resp.Body.Close()
				setProgress(1, err.Error(), false)
				return
			}
			out.Close()
			resp.Body.Close()
			if err = os.Rename(tmpPath, realPath); err != nil {
				setProgress(1, err.Error(), false)
				return
			}
			err = os.Chtimes(realPath, mod, mod)
			if err != nil {
				setProgress(1, err.Error(), false)
				return
			}
			wg.Done()
			loadedCount += 1
			diff := time.Now().Sub(t1)
			if diff < interval {
				interval = diff
			}
		}(sFile.Path, sFile.Path+".tmp", sFile.File.Id, sFile.ModTime)
	}
	wg.Wait()
	setProgress(0.05, "Synchronization... OK", true)
	setProgress(1, "All files up to date!", false)
}

func getFileStats(filePath string) (int64, time.Time, error) {
	stat, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		return 0, time.Now(), err
	}

	size := stat.Size()
	time := stat.ModTime()
	return size, time, nil
}

func getFileMd5(filePath string) string {
	f, err := os.Open(filePath)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		log.Fatal(err)
	}

	return string(h.Sum(nil))
}

type WriteCounter struct {
	Total  uint64
	Logger func(uint64)
}

func (wc *WriteCounter) Write(p []byte) (int, error) {
	n := len(p)
	wc.Total += uint64(n)
	wc.Logger(wc.Total)
	return n, nil
}
