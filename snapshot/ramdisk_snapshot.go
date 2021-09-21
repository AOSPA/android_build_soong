// Copyright 2021 The Android Open Source Project
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
package snapshot

import "android/soong/android"

// Interface for modules which can be captured in the ramdisk snapshot.
type RamdiskSnapshotModuleInterface interface {
	SnapshotModuleInterfaceBase
	InRamdisk() bool
	ExcludeFromRamdiskSnapshot() bool
}

var ramdiskSnapshotSingleton = SnapshotSingleton{
	"ramdisk",                     // name
	"SOONG_RAMDISK_SNAPSHOT_ZIP",  // makeVar
	android.OptionalPath{},        // snapshotZipFile
	RamdiskSnapshotImageSingleton, // Image
	false,                         // Fake
}

var ramdiskFakeSnapshotSingleton = SnapshotSingleton{
	"ramdisk",                         // name
	"SOONG_RAMDISK_FAKE_SNAPSHOT_ZIP", // makeVar
	android.OptionalPath{},            // snapshotZipFile
	RamdiskSnapshotImageSingleton,     // Image
	true,                              // Fake
}

func RamdiskSnapshotSingleton() android.Singleton {
	return &ramdiskSnapshotSingleton
}

func RamdiskFakeSnapshotSingleton() android.Singleton {
	return &ramdiskFakeSnapshotSingleton
}

// Determine if a dir under source tree is an SoC-owned proprietary directory based
// on ramdisk snapshot configuration
// Examples: device/, ramdisk/
func isRamdiskProprietaryPath(dir string, deviceConfig android.DeviceConfig) bool {
	return RamdiskSnapshotSingleton().(*SnapshotSingleton).Image.IsProprietaryPath(dir, deviceConfig)
}

func IsRamdiskProprietaryModule(ctx android.BaseModuleContext) bool {
	// Any module in a ramdisk proprietary path is a ramdisk proprietary
	// module.
	if isRamdiskProprietaryPath(ctx.ModuleDir(), ctx.DeviceConfig()) {
		return true
	}

	// However if the module is not in a ramdisk proprietary path, it may
	// still be a ramdisk proprietary module. This happens for cc modules
	// that are excluded from the ramdisk snapshot, and it means that the
	// ramdisk has assumed control of the framework-provided module.
	if c, ok := ctx.Module().(RamdiskSnapshotModuleInterface); ok {
		if c.ExcludeFromRamdiskSnapshot() {
			return true
		}
	}

	return false
}

var RamdiskSnapshotImageName = "ramdisk"

type RamdiskSnapshotImage struct{}

func (RamdiskSnapshotImage) Init(ctx android.RegistrationContext) {
	ctx.RegisterSingletonType("ramdisk-snapshot", RamdiskSnapshotSingleton)
	ctx.RegisterSingletonType("ramdisk-fake-snapshot", RamdiskFakeSnapshotSingleton)
}

func (RamdiskSnapshotImage) RegisterAdditionalModule(ctx android.RegistrationContext, name string, factory android.ModuleFactory) {
	ctx.RegisterModuleType(name, factory)
}

func (RamdiskSnapshotImage) shouldGenerateSnapshot(ctx android.SingletonContext) bool {
	// BOARD_VNDK_VERSION must be set to 'current' in order to generate a snapshot.
	return ctx.DeviceConfig().VndkVersion() == "current"
}

func (RamdiskSnapshotImage) InImage(m SnapshotModuleInterfaceBase) func() bool {
	v, ok := m.(RamdiskSnapshotModuleInterface)

	if !ok {
		// This module does not support Ramdisk snapshot
		return func() bool { return false }
	}

	return v.InRamdisk
}

func (RamdiskSnapshotImage) IsProprietaryPath(dir string, deviceConfig android.DeviceConfig) bool {
	return isDirectoryExcluded(dir, deviceConfig.RamdiskSnapshotDirsExcludedMap(), deviceConfig.RamdiskSnapshotDirsIncludedMap())
}

func (RamdiskSnapshotImage) ExcludeFromSnapshot(m SnapshotModuleInterfaceBase) bool {
	v, ok := m.(RamdiskSnapshotModuleInterface)

	if !ok {
		// This module does not support Ramdisk snapshot
		return true
	}

	return v.ExcludeFromRamdiskSnapshot()
}

func (RamdiskSnapshotImage) IsUsingSnapshot(cfg android.DeviceConfig) bool {
	vndkVersion := cfg.VndkVersion()
	return vndkVersion != "current" && vndkVersion != ""
}

func (RamdiskSnapshotImage) TargetSnapshotVersion(cfg android.DeviceConfig) string {
	return cfg.VndkVersion()
}

// returns true iff a given module SHOULD BE EXCLUDED, false if included
func (RamdiskSnapshotImage) ExcludeFromDirectedSnapshot(cfg android.DeviceConfig, name string) bool {
	// If we're using full snapshot, not directed snapshot, capture every module
	if !cfg.DirectedRamdiskSnapshot() {
		return false
	}
	// Else, checks if name is in RAMDISK_SNAPSHOT_MODULES.
	return !cfg.RamdiskSnapshotModules()[name]
}

func (RamdiskSnapshotImage) ImageName() string {
	return RamdiskSnapshotImageName
}

var RamdiskSnapshotImageSingleton RamdiskSnapshotImage

func init() {
	RamdiskSnapshotImageSingleton.Init(android.InitRegistrationContext)
}
