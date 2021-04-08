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
	//"io/ioutil"
	"os"
	//"path"
	//"path/filepath"
	"strconv"
	"strings"

	"android/soong/android"
	"android/soong/remoteexec"
)

var (
	// Flags used by lots of devices.  Putting them in package static variables
	// will save bytes in build.ninja so they aren't repeated for every file
	commonGlobalCflags = []string{
		"-DANDROID",
		"-fmessage-length=0",
		"-W",
		"-Wall",
		"-Wno-unused",
		"-Winit-self",
		"-Wpointer-arith",

		// Make paths in deps files relative
		"-no-canonical-prefixes",
		"-fno-canonical-system-headers",

		"-DNDEBUG",
		"-UDEBUG",

		"-fno-exceptions",
		"-Wno-multichar",

		"-O2",
		"-g",

		"-fno-strict-aliasing",

		"-Werror=date-time",
		"-Werror=pragma-pack",
		"-Werror=pragma-pack-suspicious-include",
	}

	commonGlobalConlyflags = []string{}

	deviceGlobalCflags = []string{
		"-fdiagnostics-color",

		"-ffunction-sections",
		"-fdata-sections",
		"-fno-short-enums",
		"-funwind-tables",
		"-fstack-protector-strong",
		"-Wa,--noexecstack",
		"-D_FORTIFY_SOURCE=2",

		"-Wstrict-aliasing=2",

		"-Werror=return-type",
		"-Werror=non-virtual-dtor",
		"-Werror=address",
		"-Werror=sequence-point",
		"-Werror=format-security",
	}

	deviceGlobalCppflags = []string{
		"-fvisibility-inlines-hidden",
	}

	deviceGlobalLdflags = []string{
		"-Wl,-z,noexecstack",
		"-Wl,-z,relro",
		"-Wl,-z,now",
		"-Wl,--build-id=md5",
		"-Wl,--warn-shared-textrel",
		"-Wl,--fatal-warnings",
		"-Wl,--no-undefined-version",
		"-Wl,--exclude-libs,libgcc.a",
		"-Wl,--exclude-libs,libgcc_stripped.a",
		"-Wl,--exclude-libs,libunwind_llvm.a",
	}

	deviceGlobalLldflags = append(ClangFilterUnknownLldflags(deviceGlobalLdflags),
		[]string{
			"-fuse-ld=lld",
		}...)

	hostGlobalCflags = []string{}

	hostGlobalCppflags = []string{}

	hostGlobalLdflags = []string{}

	hostGlobalLldflags = []string{"-fuse-ld=lld"}

	commonGlobalCppflags = []string{
		"-Wsign-promo",
	}

	noOverrideGlobalCflags = []string{
		"-Werror=int-to-pointer-cast",
		"-Werror=pointer-to-int-cast",
		// SDClang does not support -Werror=fortify-source.
		// TODO: b/142476859
		// "-Werror=fortify-source",
	}

	IllegalFlags = []string{
		"-w",
	}

	CStdVersion               = "gnu99"
	CppStdVersion             = "gnu++17"
	ExperimentalCStdVersion   = "gnu11"
	ExperimentalCppStdVersion = "gnu++2a"

	NdkMaxPrebuiltVersionInt = 27

	SDClang                  = false
	SDClangPath              = ""
	ForceSDClangOff          = false

	// prebuilts/clang default settings.
	ClangDefaultBase         = "prebuilts/clang/host"
	ClangDefaultVersion      = "clang-r383902b1"
	ClangDefaultShortVersion = "11.0.2"

	// Directories with warnings from Android.bp files.
	WarningAllowedProjects = []string{
		"device/",
		"vendor/",
	}

	// Directories with warnings from Android.mk files.
	WarningAllowedOldProjects = []string{}
)

var pctx = android.NewPackageContext("android/soong/cc/config")

func init() {
	if android.BuildOs == android.Linux {
		commonGlobalCflags = append(commonGlobalCflags, "-fdebug-prefix-map=/proc/self/cwd=")
	}

	pctx.StaticVariable("CommonGlobalConlyflags", strings.Join(commonGlobalConlyflags, " "))
	pctx.StaticVariable("DeviceGlobalCppflags", strings.Join(deviceGlobalCppflags, " "))
	pctx.StaticVariable("DeviceGlobalLdflags", strings.Join(deviceGlobalLdflags, " "))
	pctx.StaticVariable("DeviceGlobalLldflags", strings.Join(deviceGlobalLldflags, " "))
	pctx.StaticVariable("HostGlobalCppflags", strings.Join(hostGlobalCppflags, " "))
	pctx.StaticVariable("HostGlobalLdflags", strings.Join(hostGlobalLdflags, " "))
	pctx.StaticVariable("HostGlobalLldflags", strings.Join(hostGlobalLldflags, " "))

	pctx.VariableFunc("CommonClangGlobalCflags", func(ctx android.PackageVarContext) string {
		flags := ClangFilterUnknownCflags(commonGlobalCflags)
		flags = append(flags, "${ClangExtraCflags}")

		// http://b/131390872
		// Automatically initialize any uninitialized stack variables.
		// Prefer zero-init if multiple options are set.
		if ctx.Config().IsEnvTrue("AUTO_ZERO_INITIALIZE") {
			flags = append(flags, "-ftrivial-auto-var-init=zero -enable-trivial-auto-var-init-zero-knowing-it-will-be-removed-from-clang")
		} else if ctx.Config().IsEnvTrue("AUTO_PATTERN_INITIALIZE") {
			flags = append(flags, "-ftrivial-auto-var-init=pattern")
		} else if ctx.Config().IsEnvTrue("AUTO_UNINITIALIZE") {
			flags = append(flags, "-ftrivial-auto-var-init=uninitialized")
		} else {
			// Default to zero initialization.
			flags = append(flags, "-ftrivial-auto-var-init=zero -enable-trivial-auto-var-init-zero-knowing-it-will-be-removed-from-clang")
		}

		return strings.Join(flags, " ")
	})

	pctx.VariableFunc("DeviceClangGlobalCflags", func(ctx android.PackageVarContext) string {
		if ctx.Config().Fuchsia() {
			return strings.Join(ClangFilterUnknownCflags(deviceGlobalCflags), " ")
		} else {
			return strings.Join(append(ClangFilterUnknownCflags(deviceGlobalCflags), "${ClangExtraTargetCflags}"), " ")
		}
	})
	pctx.StaticVariable("HostClangGlobalCflags",
		strings.Join(ClangFilterUnknownCflags(hostGlobalCflags), " "))
	pctx.StaticVariable("NoOverrideClangGlobalCflags",
		strings.Join(append(ClangFilterUnknownCflags(noOverrideGlobalCflags), "${ClangExtraNoOverrideCflags}"), " "))

	pctx.StaticVariable("CommonClangGlobalCppflags",
		strings.Join(append(ClangFilterUnknownCflags(commonGlobalCppflags), "${ClangExtraCppflags}"), " "))

	pctx.StaticVariable("ClangExternalCflags", "${ClangExtraExternalCflags}")

	// Everything in these lists is a crime against abstraction and dependency tracking.
	// Do not add anything to this list.
	pctx.PrefixedExistentPathsForSourcesVariable("CommonGlobalIncludes", "-I",
		[]string{
			"system/core/include",
			"system/media/audio/include",
			"hardware/libhardware/include",
			"hardware/libhardware_legacy/include",
			"hardware/ril/include",
			"frameworks/native/include",
			"frameworks/native/opengl/include",
			"frameworks/av/include",
		})
	// This is used by non-NDK modules to get jni.h. export_include_dirs doesn't help
	// with this, since there is no associated library.
	pctx.PrefixedExistentPathsForSourcesVariable("CommonNativehelperInclude", "-I",
		[]string{"libnativehelper/include_jni"})

	setSdclangVars()

	pctx.SourcePathVariable("ClangDefaultBase", ClangDefaultBase)
	pctx.VariableFunc("ClangBase", func(ctx android.PackageVarContext) string {
		if override := ctx.Config().Getenv("LLVM_PREBUILTS_BASE"); override != "" {
			return override
		}
		return "${ClangDefaultBase}"
	})
	pctx.VariableFunc("ClangVersion", func(ctx android.PackageVarContext) string {
		if override := ctx.Config().Getenv("LLVM_PREBUILTS_VERSION"); override != "" {
			return override
		}
		return ClangDefaultVersion
	})
	pctx.StaticVariable("ClangPath", "${ClangBase}/${HostPrebuiltTag}/${ClangVersion}")
	pctx.StaticVariable("ClangBin", "${ClangPath}/bin")

	pctx.VariableFunc("ClangShortVersion", func(ctx android.PackageVarContext) string {
		if override := ctx.Config().Getenv("LLVM_RELEASE_VERSION"); override != "" {
			return override
		}
		return ClangDefaultShortVersion
	})
	pctx.StaticVariable("ClangAsanLibDir", "${ClangBase}/linux-x86/${ClangVersion}/lib64/clang/${ClangShortVersion}/lib/linux")

	// These are tied to the version of LLVM directly in external/llvm, so they might trail the host prebuilts
	// being used for the rest of the build process.
	pctx.SourcePathVariable("RSClangBase", "prebuilts/clang/host")
	pctx.SourcePathVariable("RSClangVersion", "clang-3289846")
	pctx.SourcePathVariable("RSReleaseVersion", "3.8")
	pctx.StaticVariable("RSLLVMPrebuiltsPath", "${RSClangBase}/${HostPrebuiltTag}/${RSClangVersion}/bin")
	pctx.StaticVariable("RSIncludePath", "${RSLLVMPrebuiltsPath}/../lib64/clang/${RSReleaseVersion}/include")

	pctx.PrefixedExistentPathsForSourcesVariable("RsGlobalIncludes", "-I",
		[]string{
			"external/clang/lib/Headers",
			"frameworks/rs/script_api/include",
		})

	pctx.VariableFunc("CcWrapper", func(ctx android.PackageVarContext) string {
		if override := ctx.Config().Getenv("CC_WRAPPER"); override != "" {
			return override + " "
		}
		return ""
	})

	pctx.VariableFunc("RECXXPool", remoteexec.EnvOverrideFunc("RBE_CXX_POOL", remoteexec.DefaultPool))
	pctx.VariableFunc("RECXXLinksPool", remoteexec.EnvOverrideFunc("RBE_CXX_LINKS_POOL", remoteexec.DefaultPool))
	pctx.VariableFunc("RECXXLinksExecStrategy", remoteexec.EnvOverrideFunc("RBE_CXX_LINKS_EXEC_STRATEGY", remoteexec.LocalExecStrategy))
	pctx.VariableFunc("REAbiDumperExecStrategy", remoteexec.EnvOverrideFunc("RBE_ABI_DUMPER_EXEC_STRATEGY", remoteexec.LocalExecStrategy))
	pctx.VariableFunc("REAbiLinkerExecStrategy", remoteexec.EnvOverrideFunc("RBE_ABI_LINKER_EXEC_STRATEGY", remoteexec.LocalExecStrategy))
}

func setSdclangVars() {
	sdclangPath := ""
	sdclangAEFlag := ""
	sdclangFlags := ""

	product := android.SdclangEnv["TARGET_PRODUCT"]
	aeConfigPath := android.SdclangEnv["SDCLANG_AE_CONFIG"]
	sdclangConfigPath := android.SdclangEnv["SDCLANG_CONFIG"]
	sdclangSA := android.SdclangEnv["SDCLANG_SA_ENABLED"]

	type sdclangAEConfig struct {
		SDCLANG_AE_FLAG string
	}

	// Load AE config file and set AE flag
	if file, err := os.Open(aeConfigPath); err == nil {
		decoder := json.NewDecoder(file)
		aeConfig := sdclangAEConfig{}
		if err := decoder.Decode(&aeConfig); err == nil {
			sdclangAEFlag = aeConfig.SDCLANG_AE_FLAG
		} else {
			panic(err)
		}
	}

	// Load SD Clang config file and set SD Clang variables
	var sdclangConfig interface{}
	if file, err := os.Open(sdclangConfigPath); err == nil {
		decoder := json.NewDecoder(file)
                // Parse the config file
		if err := decoder.Decode(&sdclangConfig); err == nil {
			config := sdclangConfig.(map[string]interface{})
			// Retrieve the default block
			if dev, ok := config["default"]; ok {
				devConfig := dev.(map[string]interface{})
				// FORCE_SDCLANG_OFF is required in the default block
				if _, ok := devConfig["FORCE_SDCLANG_OFF"]; ok {
					ForceSDClangOff = devConfig["FORCE_SDCLANG_OFF"].(bool)
				}
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
				// SDCLANG_FLAGS is optional in the default block
				if _, ok := devConfig["SDCLANG_FLAGS"]; ok {
					sdclangFlags = devConfig["SDCLANG_FLAGS"].(string)
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
				// SDCLANG_FLAGS is optional in the device specific block
				if _, ok := devConfig["SDCLANG_FLAGS"]; ok {
					sdclangFlags = devConfig["SDCLANG_FLAGS"].(string)
				}
			}
			b, _ := strconv.ParseBool(sdclangSA)
			if(b) {
				llvmsa_loc := "llvmsa"
				s := []string{sdclangFlags, "--compile-and-analyze", llvmsa_loc}
				sdclangFlags = strings.Join(s, " ")
				fmt.Println("Clang SA is enabled: ", sdclangFlags)
			} else {
				fmt.Println("Clang SA is not enabled")
			}
		} else {
			panic(err)
		}
	} else {
		fmt.Println(err)
	}

	// Override SDCLANG if the varialbe is set in the environment
	if sdclang := android.SdclangEnv["SDCLANG"]; sdclang != "" {
		if override, err := strconv.ParseBool(sdclang); err == nil {
			SDClang = override
		}
	}

	// Sanity check SDCLANG_PATH
	if envPath := android.SdclangEnv["SDCLANG_PATH"]; sdclangPath == "" && envPath == "" {
		panic("SDCLANG_PATH can not be empty")
	}

	// Override SDCLANG_PATH if the variable is set in the environment
	pctx.VariableFunc("SDClangBin", func(ctx android.PackageVarContext) string {
		if override := ctx.Config().Getenv("SDCLANG_PATH"); override != "" {
			return override
		}
		return sdclangPath
	})

	// Override SDCLANG_COMMON_FLAGS if the variable is set in the environment
	pctx.VariableFunc("SDClangFlags", func(ctx android.PackageVarContext) string {
		if override := ctx.Config().Getenv("SDCLANG_COMMON_FLAGS"); override != "" {
			return override
		}
		return sdclangAEFlag + " " + sdclangFlags
	})

	SDClangPath = sdclangPath
	// Find the path to SDLLVM's ASan libraries
	// TODO (b/117846004): Disable setting SDClangAsanLibDir due to unit test path issues
	//absPath := sdclangPath
	//if envPath := android.SdclangEnv["SDCLANG_PATH"]; envPath != "" {
	//	absPath = envPath
	//}
	//if !filepath.IsAbs(absPath) {
	//	absPath = path.Join(androidRoot, absPath)
	//}
	//
	//libDirPrefix := "../lib/clang"
	//libDir, err := ioutil.ReadDir(path.Join(absPath, libDirPrefix))
	//if err != nil {
	//	libDirPrefix = "../lib64/clang"
	//	libDir, err = ioutil.ReadDir(path.Join(absPath, libDirPrefix))
	//}
	//if err != nil {
	//	panic(err)
	//}
	//if len(libDir) != 1 || !libDir[0].IsDir() {
	//	panic("Failed to find sanitizer libraries")
	//}
	//
	//pctx.StaticVariable("SDClangAsanLibDir", path.Join(absPath, libDirPrefix, libDir[0].Name(), "lib/linux"))
}

var HostPrebuiltTag = pctx.VariableConfigMethod("HostPrebuiltTag", android.Config.PrebuiltOS)

func bionicHeaders(kernelArch string) string {
	return strings.Join([]string{
		"-isystem bionic/libc/include",
		"-isystem bionic/libc/kernel/uapi",
		"-isystem bionic/libc/kernel/uapi/asm-" + kernelArch,
		"-isystem bionic/libc/kernel/android/scsi",
		"-isystem bionic/libc/kernel/android/uapi",
	}, " ")
}

func envOverrideFunc(envVar, defaultVal string) func(ctx android.PackageVarContext) string {
	return func(ctx android.PackageVarContext) string {
		if override := ctx.Config().Getenv(envVar); override != "" {
			return override
		}
		return defaultVal
	}
}
