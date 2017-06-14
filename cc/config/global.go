// Copyright 2016 Google Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"

	"android/soong/android"
)

// Flags used by lots of devices.  Putting them in package static variables will save bytes in
// build.ninja so they aren't repeated for every file
var (
	commonGlobalCflags = []string{
		"-DANDROID",
		"-fmessage-length=0",
		"-W",
		"-Wall",
		"-Wno-unused",
		"-Winit-self",
		"-Wpointer-arith",

		// COMMON_RELEASE_CFLAGS
		"-DNDEBUG",
		"-UDEBUG",
	}

	commonGlobalConlyflags = []string{}

	deviceGlobalCflags = []string{
		"-fdiagnostics-color",

		// TARGET_ERROR_FLAGS
		"-Werror=return-type",
		"-Werror=non-virtual-dtor",
		"-Werror=address",
		"-Werror=sequence-point",
		"-Werror=date-time",
	}

	hostGlobalCflags = []string{}

	commonGlobalCppflags = []string{
		"-Wsign-promo",
	}

	noOverrideGlobalCflags = []string{
		"-Werror=int-to-pointer-cast",
		"-Werror=pointer-to-int-cast",
	}

	IllegalFlags = []string{
		"-w",
	}

	CStdVersion               = "gnu99"
	CppStdVersion             = "gnu++14"
	GccCppStdVersion          = "gnu++11"
	ExperimentalCStdVersion   = "gnu11"
	ExperimentalCppStdVersion = "gnu++1z"
	SDClang                   = false
)

var pctx = android.NewPackageContext("android/soong/cc/config")

func init() {
	if android.BuildOs == android.Linux {
		commonGlobalCflags = append(commonGlobalCflags, "-fdebug-prefix-map=/proc/self/cwd=")
	}

	pctx.StaticVariable("CommonGlobalCflags", strings.Join(commonGlobalCflags, " "))
	pctx.StaticVariable("CommonGlobalConlyflags", strings.Join(commonGlobalConlyflags, " "))
	pctx.StaticVariable("DeviceGlobalCflags", strings.Join(deviceGlobalCflags, " "))
	pctx.StaticVariable("HostGlobalCflags", strings.Join(hostGlobalCflags, " "))
	pctx.StaticVariable("NoOverrideGlobalCflags", strings.Join(noOverrideGlobalCflags, " "))

	pctx.StaticVariable("CommonGlobalCppflags", strings.Join(commonGlobalCppflags, " "))

	pctx.StaticVariable("CommonClangGlobalCflags",
		strings.Join(append(ClangFilterUnknownCflags(commonGlobalCflags), "${ClangExtraCflags}"), " "))
	pctx.StaticVariable("DeviceClangGlobalCflags",
		strings.Join(append(ClangFilterUnknownCflags(deviceGlobalCflags), "${ClangExtraTargetCflags}"), " "))
	pctx.StaticVariable("HostClangGlobalCflags",
		strings.Join(ClangFilterUnknownCflags(hostGlobalCflags), " "))
	pctx.StaticVariable("NoOverrideClangGlobalCflags",
		strings.Join(append(ClangFilterUnknownCflags(noOverrideGlobalCflags), "${ClangExtraNoOverrideCflags}"), " "))

	pctx.StaticVariable("CommonClangGlobalCppflags",
		strings.Join(append(ClangFilterUnknownCflags(commonGlobalCppflags), "${ClangExtraCppflags}"), " "))

	// Everything in these lists is a crime against abstraction and dependency tracking.
	// Do not add anything to this list.
	pctx.PrefixedPathsForOptionalSourceVariable("CommonGlobalIncludes", "-I",
		[]string{
			"system/core/include",
			"system/media/audio/include",
			"hardware/libhardware/include",
			"hardware/libhardware_legacy/include",
			"hardware/ril/include",
			"libnativehelper/include",
			"frameworks/native/include",
			"frameworks/native/opengl/include",
		})
	pctx.PrefixedPathsForOptionalSourceVariable("CommonGlobalSystemIncludes", "-isystem ",
		[]string{
			"frameworks/av/include",
		})
	// This is used by non-NDK modules to get jni.h. export_include_dirs doesn't help
	// with this, since there is no associated library.
	pctx.PrefixedPathsForOptionalSourceVariable("CommonNativehelperInclude", "-I",
		[]string{"libnativehelper/include/nativehelper"})

	pctx.SourcePathVariable("ClangDefaultBase", "prebuilts/clang/host")
	pctx.VariableFunc("ClangBase", func(config interface{}) (string, error) {
		if override := config.(android.Config).Getenv("LLVM_PREBUILTS_BASE"); override != "" {
			return override, nil
		}
		return "${ClangDefaultBase}", nil
	})
	pctx.VariableFunc("ClangVersion", func(config interface{}) (string, error) {
		if override := config.(android.Config).Getenv("LLVM_PREBUILTS_VERSION"); override != "" {
			return override, nil
		}
		return "clang-3859424", nil
	})
	pctx.StaticVariable("ClangPath", "${ClangBase}/${HostPrebuiltTag}/${ClangVersion}")
	pctx.StaticVariable("ClangBin", "${ClangPath}/bin")

	pctx.VariableFunc("ClangShortVersion", func(config interface{}) (string, error) {
		if override := config.(android.Config).Getenv("LLVM_RELEASE_VERSION"); override != "" {
			return override, nil
		}
		return "4.0", nil
	})
	pctx.StaticVariable("ClangAsanLibDir", "${ClangPath}/lib64/clang/${ClangShortVersion}/lib/linux")

	// These are tied to the version of LLVM directly in external/llvm, so they might trail the host prebuilts
	// being used for the rest of the build process.
	pctx.SourcePathVariable("RSClangBase", "prebuilts/clang/host")
	pctx.SourcePathVariable("RSClangVersion", "clang-3289846")
	pctx.SourcePathVariable("RSReleaseVersion", "3.8")
	pctx.StaticVariable("RSLLVMPrebuiltsPath", "${RSClangBase}/${HostPrebuiltTag}/${RSClangVersion}/bin")
	pctx.StaticVariable("RSIncludePath", "${RSLLVMPrebuiltsPath}/../lib64/clang/${RSReleaseVersion}/include")

	pctx.VariableFunc("CcWrapper", func(config interface{}) (string, error) {
		if override := config.(android.Config).Getenv("CC_WRAPPER"); override != "" {
			return override + " ", nil
		}
		return "", nil
	})

	setSdclangVars()
}

func setSdclangVars() {
	sdclangPath := ""
	sdclangPath2 := ""
	sdclangAEFlag := ""
	sdclangFlags := ""
	sdclangFlags2 := ""

	product := os.Getenv("TARGET_PRODUCT")
	androidRoot := os.Getenv("ANDROID_BUILD_TOP")
	aeConfigPath := os.Getenv("SDCLANG_AE_CONFIG")
	sdclangConfigPath := os.Getenv("SDCLANG_CONFIG")

	type sdclangAEConfig struct {
		SDCLANG_AE_FLAG string
	}

	// Load AE config file and set AE flag
	aeConfigFile := path.Join(androidRoot, aeConfigPath)
	if file, err := os.Open(aeConfigFile); err == nil {
		decoder := json.NewDecoder(file)
		aeConfig := sdclangAEConfig{}
		if err := decoder.Decode(&aeConfig); err == nil {
			sdclangAEFlag = aeConfig.SDCLANG_AE_FLAG
		} else {
			panic(err)
		}
	}

	// Load SD Clang config file and set SD Clang variables
	sdclangConfigFile := path.Join(androidRoot, sdclangConfigPath)
	var sdclangConfig interface{}
	if file, err := os.Open(sdclangConfigFile); err == nil {
		decoder := json.NewDecoder(file)
                // Parse the config file
		if err := decoder.Decode(&sdclangConfig); err == nil {
			config := sdclangConfig.(map[string]interface{})
			// Retrieve the default block
			if dev, ok := config["default"]; ok {
				devConfig := dev.(map[string]interface{})
				// SDCLANG is optional in the default block
				if _, ok := devConfig["SDCLANG"]; ok {
					SDClang = devConfig["SDCLANG"].(bool)
				}
				// SDCLANG_PATH is required in the default block
				if _, ok := devConfig["SDCLANG_PATH"]; ok {
					sdclangPath = devConfig["SDCLANG_PATH"].(string)
				} else {
					panic("SDCLANG_PATH is required in the default block")
				}
				// SDCLANG_PATH_2 is required in the default block
				if _, ok := devConfig["SDCLANG_PATH_2"]; ok {
					sdclangPath2 = devConfig["SDCLANG_PATH_2"].(string)
				} else {
					panic("SDCLANG_PATH_2 is required in the default block")
				}
				// SDCLANG_FLAGS is optional in the default block
				if _, ok := devConfig["SDCLANG_FLAGS"]; ok {
					sdclangFlags = devConfig["SDCLANG_FLAGS"].(string)
				}
				// SDCLANG_FLAGS_2 is optional in the default block
				if _, ok := devConfig["SDCLANG_FLAGS_2"]; ok {
					sdclangFlags2 = devConfig["SDCLANG_FLAGS_2"].(string)
				}
			} else {
				panic("Default block is required in the SD Clang config file")
			}
			// Retrieve the device specific block if it exists in the config file
			if dev, ok := config[product]; ok {
				devConfig := dev.(map[string]interface{})
				// SDCLANG is optional in the device specific block
				if _, ok := devConfig["SDCLANG"]; ok {
					SDClang = devConfig["SDCLANG"].(bool)
				}
				// SDCLANG_PATH is optional in the device specific block
				if _, ok := devConfig["SDCLANG_PATH"]; ok {
					sdclangPath = devConfig["SDCLANG_PATH"].(string)
				}
				// SDCLANG_PATH_2 is optional in the device specific block
				if _, ok := devConfig["SDCLANG_PATH_2"]; ok {
					sdclangPath2 = devConfig["SDCLANG_PATH_2"].(string)
				}
				// SDCLANG_FLAGS is optional in the device specific block
				if _, ok := devConfig["SDCLANG_FLAGS"]; ok {
					sdclangFlags = devConfig["SDCLANG_FLAGS"].(string)
				}
				// SDCLANG_FLAGS_2 is optional in the device specific block
				if _, ok := devConfig["SDCLANG_FLAGS_2"]; ok {
					sdclangFlags2 = devConfig["SDCLANG_FLAGS_2"].(string)
				}
			}
		} else {
			panic(err)
		}
	} else {
		fmt.Println(err)
	}

	// Override SDCLANG if the varialbe is set in the environment
	if sdclang := os.Getenv("SDCLANG"); sdclang != "" {
		if override, err := strconv.ParseBool(sdclang); err == nil {
			SDClang = override
		}
	}

	// Sanity check SDCLANG_PATH
	if envPath := os.Getenv("SDCLANG_PATH"); sdclangPath == "" && envPath == "" {
		panic("SDCLANG_PATH can not be empty")
	}

	// Sanity check SDCLANG_PATH_2
	if envPath := os.Getenv("SDCLANG_PATH_2"); sdclangPath2 == "" && envPath == "" {
		panic("SDCLANG_PATH_2 can not be empty")
	}

	// Override SDCLANG_PATH if the variable is set in the environment
	pctx.VariableFunc("SDClangBin", func(config interface{}) (string, error) {
		if override := config.(android.Config).Getenv("SDCLANG_PATH"); override != "" {
			return override, nil
		}
		return sdclangPath, nil
	})

	// Override SDCLANG_PATH_2 if the variable is set in the environment
	pctx.VariableFunc("SDClangBin2", func(config interface{}) (string, error) {
		if override := config.(android.Config).Getenv("SDCLANG_PATH_2"); override != "" {
			return override, nil
		}
		return sdclangPath2, nil
	})

	// Override SDCLANG_COMMON_FLAGS if the variable is set in the environment
	pctx.VariableFunc("SDClangFlags", func(config interface{}) (string, error) {
		if override := config.(android.Config).Getenv("SDCLANG_COMMON_FLAGS"); override != "" {
			return override, nil
		}
		return sdclangAEFlag + " " + sdclangFlags, nil
	})

	// Override SDCLANG_COMMON_FLAGS_2 if the variable is set in the environment
	pctx.VariableFunc("SDClangFlags2", func(config interface{}) (string, error) {
		if override := config.(android.Config).Getenv("SDCLANG_COMMON_FLAGS_2"); override != "" {
			return override, nil
		}
		return sdclangAEFlag + " " + sdclangFlags2, nil
	})
}

var HostPrebuiltTag = pctx.VariableConfigMethod("HostPrebuiltTag", android.Config.PrebuiltOS)

func bionicHeaders(bionicArch, kernelArch string) string {
	return strings.Join([]string{
		"-isystem bionic/libc/arch-" + bionicArch + "/include",
		"-isystem bionic/libc/include",
		"-isystem bionic/libc/kernel/uapi",
		"-isystem bionic/libc/kernel/uapi/asm-" + kernelArch,
		"-isystem bionic/libc/kernel/android/uapi",
	}, " ")
}

func VndkLibraries() []string {
	return []string{}
}

// This needs to be kept up to date with the list in system/core/rootdir/etc/ld.config.txt:
// [vendor]
// namespace.default.link.system.shared_libs
func LLndkLibraries() []string {
	return []string{"libc", "libm", "libdl", "liblog", "libandroid_net", "ld-android"}
}
