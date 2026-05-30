package updater

type Manifest struct {
	AppList AppList `json:"appList"`
	Ret     string  `json:"ret"`
}

type ReleaseNote struct {
	AppName     string `json:"appName"`
	Description string `json:"description"`
	Version     string `json:"version"`
}

type File struct {
	Path        string `json:"path"`
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	Sha256      string `json:"sha256"`
	DownloadURL string `json:"downloadURL"`
}

type AppList struct {
	FileName    string      `json:"fileName"`
	ReleaseNote ReleaseNote `json:"ReleaseNote"`
	FileList    []File      `json:"fileList"`
}

type Result struct {
	Total      int
	Downloaded int
	Skipped    int
	Failed     []FileError
}

type FileError struct {
	File File
	Err  error
}

func (e FileError) Error() string {
	if e.Err == nil {
		return e.File.Name
	}
	return e.File.Name + ": " + e.Err.Error()
}

type UpdateError struct {
	Failures []FileError
}

func (e UpdateError) Error() string {
	if len(e.Failures) == 0 {
		return "update failed"
	}
	if len(e.Failures) == 1 {
		return e.Failures[0].Error()
	}
	return e.Failures[0].Error() + " and other failures"
}
