package main

import (
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"

	rc_lib "android/soong/cmd/release_config/release_config_lib"
	rc_proto "android/soong/cmd/release_config/release_config_proto"
)

type Flags struct {
	// The path to the top of the workspace.  Default: ".".
	top string

	// Output file.
	output string

	// Format for output file
	format string

	// List of release config directories to process.
	dirs rc_lib.StringList

	// Disable warning messages
	quiet bool

	// Panic on errors.
	debug bool
}

func sortDirectories(dirList []string) {
	order := func(dir string) int {
		switch {
		// These three are always in this order.
		case dir == "build/release":
			return 1
		case dir == "vendor/google_shared/build/release":
			return 2
		case dir == "vendor/google/release":
			return 3
		// Keep their subdirs in the same order.
		case strings.HasPrefix(dir, "build/release/"):
			return 21
		case strings.HasPrefix(dir, "vendor/google_shared/build/release/"):
			return 22
		case strings.HasPrefix(dir, "vendor/google/release/"):
			return 23
		// Everything else sorts by directory path.
		default:
			return 99
		}
	}

	slices.SortFunc(dirList, func(a, b string) int {
		aOrder, bOrder := order(a), order(b)
		if aOrder != bOrder {
			return aOrder - bOrder
		}
		return strings.Compare(a, b)
	})
}

func main() {
	var flags Flags
	topDir, err := rc_lib.GetTopDir()

	// Handle the common arguments
	flag.StringVar(&flags.top, "top", topDir, "path to top of workspace")
	flag.Var(&flags.dirs, "dir", "path to a release config contribution directory. May be repeated")
	flag.StringVar(&flags.format, "format", "pb", "output file format")
	flag.StringVar(&flags.output, "output", "release_config_contributions.pb", "output file")
	flag.BoolVar(&flags.debug, "debug", false, "turn on debugging output for errors")
	flag.BoolVar(&flags.quiet, "quiet", false, "disable warning messages")
	flag.Parse()

	errorExit := func(err error) {
		if flags.debug {
			panic(err)
		}
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}

	if flags.quiet {
		rc_lib.DisableWarnings()
	}

	if err = os.Chdir(flags.top); err != nil {
		errorExit(err)
	}

	contributingDirsMap := make(map[string][]string)
	for _, dir := range flags.dirs {
		contributions, err := rc_lib.EnumerateReleaseConfigs(dir)
		if err != nil {
			errorExit(err)
		}
		for _, name := range contributions {
			contributingDirsMap[name] = append(contributingDirsMap[name], dir)
		}
	}

	releaseConfigNames := []string{}
	for name := range contributingDirsMap {
		releaseConfigNames = append(releaseConfigNames, name)
	}
	slices.Sort(releaseConfigNames)

	message := &rc_proto.ReleaseConfigContributionsArtifacts{
		ReleaseConfigContributionsArtifactList: []*rc_proto.ReleaseConfigContributionsArtifact{},
	}
	for _, name := range releaseConfigNames {
		dirs := contributingDirsMap[name]
		slices.Sort(dirs)
		message.ReleaseConfigContributionsArtifactList = append(
			message.ReleaseConfigContributionsArtifactList,
			&rc_proto.ReleaseConfigContributionsArtifact{
				Name:                    &name,
				ContributingDirectories: dirs,
			})
	}

	err = rc_lib.WriteFormattedMessage(flags.output, flags.format, message)
	if err != nil {
		errorExit(err)
	}
}
