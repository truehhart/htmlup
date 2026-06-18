// Package buildinfo resolves the binary's version, commit, and build date,
// preferring values stamped in at release time (via -ldflags) and falling back
// to the VCS metadata the Go toolchain embeds in module builds.
package buildinfo

import "runtime/debug"

const devVersion = "dev"

// Info describes the build identity of the running binary.
type Info struct {
	Version  string
	Commit   string
	Date     string
	Modified bool // working tree had uncommitted changes at build time
}

// Resolve combines ldflags-stamped values with the Go build info embedded in
// the binary. Stamped values win; anything missing is backfilled from VCS data.
func Resolve(version, commit, date string) Info {
	bi, ok := debug.ReadBuildInfo()
	return resolve(version, commit, date, bi, ok)
}

func resolve(version, commit, date string, bi *debug.BuildInfo, ok bool) Info {
	info := Info{Version: version, Commit: commit, Date: date}
	if ok && bi != nil {
		if info.Version == "" || info.Version == devVersion {
			if v := bi.Main.Version; v != "" && v != "(devel)" {
				info.Version = v
			}
		}
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				if info.Commit == "" {
					info.Commit = s.Value
				}
			case "vcs.time":
				if info.Date == "" {
					info.Date = s.Value
				}
			case "vcs.modified":
				info.Modified = s.Value == "true"
			}
		}
	}
	if info.Version == "" {
		info.Version = devVersion
	}
	return info
}

// String renders the build info for `htmlup version` / `htmlup --version`.
func (i Info) String() string {
	s := "htmlup " + i.Version
	if i.Commit != "" {
		s += "\ncommit: " + i.Commit
		if i.Modified {
			s += " (modified)"
		}
	}
	if i.Date != "" {
		s += "\nbuilt:  " + i.Date
	}
	return s
}
