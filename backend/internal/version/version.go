package version

import (
	"fmt"
	"runtime"

	"encoding/json/jsontext"
	json "encoding/json/v2"
)

var (
	gitBranch = "unknown"
	gitCommit = "unknown"
	gitTag    = "unknown"
	buildUser = "unknown"
	buildDate = "unknown"
)

type Info struct {
	GitBranch string `json:"gitBranch"`
	GitCommit string `json:"gitCommit"`
	GitTag    string `json:"gitTag"`
	BuildUser string `json:"buildUser"`
	BuildDate string `json:"buildDate"`
	GoVersion string `json:"goVersion"`
	Compiler  string `json:"compiler"`
	Platform  string `json:"platform"`
}

func (info Info) String() string {
	// v2 没有 MarshalIndent,缩进作为编码选项传入
	b, _ := json.Marshal(info, jsontext.WithIndent("  "))
	return string(b)
}

func Get() Info {
	return Info{
		GitBranch: gitBranch,
		GitCommit: gitCommit,
		GitTag:    gitTag,
		BuildUser: buildUser,
		BuildDate: buildDate,
		GoVersion: runtime.Version(),
		Compiler:  runtime.Compiler,
		Platform:  fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
	}
}
