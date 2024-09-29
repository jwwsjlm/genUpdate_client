package main

type JSONData struct {
	AppList AppList `json:"appList"`
	Ret     string  `json:"ret"`
}
type ReleaseNote struct {
	AppName     string `json:"appName"`
	Description string `json:"description"`
	Version     string `json:"version"`
}
type FileList struct {
	Path        string `json:"path"`
	Name        string `json:"name"`
	Size        int    `json:"size"`
	Sha256      string `json:"sha256"`
	DownloadURL string `json:"downloadURL"`
}
type AppList struct {
	FileName    string      `json:"fileName"`
	ReleaseNote ReleaseNote `json:"ReleaseNote"`
	FileList    []FileList  `json:"fileList"`
}
