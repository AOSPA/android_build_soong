// Copyright 2020 The Android Open Source Project
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package cc

import (
	"android/soong/android"
)

var (
	headerExts = []string{".h", ".hh", ".hpp", ".hxx", ".h++", ".inl", ".inc", ".ipp", ".h.generic"}
)

type snapshotLibraryInterface interface {
	exportedFlagsProducer
	libraryInterface
	collectHeadersForSnapshot(ctx android.ModuleContext)
	snapshotHeaders() android.Paths
}

var _ snapshotLibraryInterface = (*prebuiltLibraryLinker)(nil)
var _ snapshotLibraryInterface = (*libraryDecorator)(nil)

type snapshotMap struct {
	snapshots map[string]string
}

func newSnapshotMap() *snapshotMap {
	return &snapshotMap{
		snapshots: make(map[string]string),
	}
}

func snapshotMapKey(name string, arch android.ArchType) string {
	return name + ":" + arch.String()
}

// Adds a snapshot name for given module name and architecture.
// e.g. add("libbase", X86, "libbase.vndk.29.x86")
func (s *snapshotMap) add(name string, arch android.ArchType, snapshot string) {
	s.snapshots[snapshotMapKey(name, arch)] = snapshot
}

// Returns snapshot name for given module name and architecture, if found.
// e.g. get("libcutils", X86) => "libcutils.vndk.29.x86", true
func (s *snapshotMap) get(name string, arch android.ArchType) (snapshot string, found bool) {
	snapshot, found = s.snapshots[snapshotMapKey(name, arch)]
	return snapshot, found
}

// shouldCollectHeadersForSnapshot determines if the module is a possible candidate for snapshot.
// If it's true, collectHeadersForSnapshot will be called in GenerateAndroidBuildActions.
func shouldCollectHeadersForSnapshot(ctx android.ModuleContext, m *Module) bool {
	if ctx.DeviceConfig().VndkVersion() != "current" &&
		ctx.DeviceConfig().RecoverySnapshotVersion() != "current" {
		return false
	}
	if _, _, ok := isVndkSnapshotLibrary(ctx.DeviceConfig(), m); ok {
		return ctx.Config().VndkSnapshotBuildArtifacts()
	}

	for _, image := range []snapshotImage{vendorSnapshotImageSingleton, recoverySnapshotImageSingleton} {
		if isSnapshotAware(ctx.DeviceConfig(), m, image.isProprietaryPath(ctx.ModuleDir()), image) {
			return true
		}
	}
	return false
}
