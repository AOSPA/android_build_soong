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
	"encoding/xml"
	"fmt"
	"io/ioutil"
	"os"

	//"path"
	//"path/filepath"
	"strconv"
	"strings"

	"android/soong/android"
	"android/soong/remoteexec"
)

type QiifaAbiLibs struct {
	XMLName         xml.Name `xml:"abilibs"`
        Library         []string `xml:"library"`
}

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
		"-Wunreachable-code-loop-increment",

		// Make paths in deps files relative
		"-no-canonical-prefixes",
		"-fno-canonical-system-headers",

		"-DNDEBUG",
		"-UDEBUG",

		"-fno-exceptions",
		"-Wno-multichar",

		"-O2",
		"-g",
		"-fdebug-info-for-profiling",

		"-fno-strict-aliasing",

		"-Werror=date-time",
		"-Werror=pragma-pack",
		"-Werror=pragma-pack-suspicious-include",
		"-Werror=string-plus-int",
		"-Werror=unreachable-code-loop-increment",
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
		// TODO: Eventually we should link against a libunwind.a with hidden symbols, and then these
		// --exclude-libs arguments can be removed.
		"-Wl,--exclude-libs,libgcc.a",
		"-Wl,--exclude-libs,libgcc_stripped.a",
		"-Wl,--exclude-libs,libunwind_llvm.a",
		"-Wl,--exclude-libs,libunwind.a",
		"-Wl,--icf=safe",
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
		"-Werror=bool-operation",
		"-Werror=implicit-int-float-conversion",
		"-Werror=int-in-bool-context",
		"-Werror=int-to-pointer-cast",
		"-Werror=pointer-to-int-cast",
		"-Werror=string-compare",
		"-Werror=xor-used-as-pow",
		// http://b/161386391 for -Wno-void-pointer-to-enum-cast
		"-Wno-void-pointer-to-enum-cast",
		// http://b/161386391 for -Wno-void-pointer-to-int-cast
		"-Wno-void-pointer-to-int-cast",
		// http://b/161386391 for -Wno-pointer-to-int-cast
		"-Wno-pointer-to-int-cast",
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

	SDClang             = false
	SDClangPath         = ""
	ForceSDClangOff     = false

	// prebuilts/clang default settings.
	ClangDefaultBase         = "prebuilts/clang/host"
	ClangDefaultVersion      = "clang-r416183b1"
	ClangDefaultShortVersion = "12.0.7"

	// Directories with warnings from Android.bp files.
	WarningAllowedProjects = []string{
		"device/",
		"vendor/",
	}

	// Directories with warnings from Android.mk files.
	WarningAllowedOldProjects = []string{}
        QiifaAbiLibraryList       = []string{}
)

var pctx = android.NewPackageContext("android/soong/cc/config")

func init() {
	if android.BuildOs == android.Linux {
		commonGlobalCflags = append(commonGlobalCflags, "-fdebug-prefix-map=/proc/self/cwd=")
	}
	qiifaBuildConfig := os.Getenv("QIIFA_BUILD_CONFIG")
	if _, err := os.Stat(qiifaBuildConfig); !os.IsNotExist(err) {
		data, _ := ioutil.ReadFile(qiifaBuildConfig)
		var qiifalibs QiifaAbiLibs
		_ = xml.Unmarshal([]byte(data), &qiifalibs)
                for i := 0; i < len(qiifalibs.Library); i++ {
                    QiifaAbiLibraryList = append(QiifaAbiLibraryList, qiifalibs.Library[i])

                }
        }

	staticVariableExportedToBazel("CommonGlobalConlyflags", commonGlobalConlyflags)
	staticVariableExportedToBazel("DeviceGlobalCppflags", deviceGlobalCppflags)
	staticVariableExportedToBazel("DeviceGlobalLdflags", deviceGlobalLdflags)
	staticVariableExportedToBazel("DeviceGlobalLldflags", deviceGlobalLldflags)
	staticVariableExportedToBazel("HostGlobalCppflags", hostGlobalCppflags)
	staticVariableExportedToBazel("HostGlobalLdflags", hostGlobalLdflags)
	staticVariableExportedToBazel("HostGlobalLldflags", hostGlobalLldflags)

	// Export the static default CommonClangGlobalCflags to Bazel.
	// TODO(187086342): handle cflags that are set in VariableFuncs.
	commonClangGlobalCFlags := append(
		ClangFilterUnknownCflags(commonGlobalCflags),
		[]string{
			"${ClangExtraCflags}",
			// Default to zero initialization.
			"-ftrivial-auto-var-init=zero",
			"-enable-trivial-auto-var-init-zero-knowing-it-will-be-removed-from-clang",
		}...)
	exportedVars.Set("CommonClangGlobalCflags", variableValue(commonClangGlobalCFlags))

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

	// Export the static default DeviceClangGlobalCflags to Bazel.
	// TODO(187086342): handle cflags that are set in VariableFuncs.
	deviceClangGlobalCflags := append(ClangFilterUnknownCflags(deviceGlobalCflags), "${ClangExtraTargetCflags}")
	exportedVars.Set("DeviceClangGlobalCflags", variableValue(deviceClangGlobalCflags))

	pctx.VariableFunc("DeviceClangGlobalCflags", func(ctx android.PackageVarContext) string {
		if ctx.Config().Fuchsia() {
			return strings.Join(ClangFilterUnknownCflags(deviceGlobalCflags), " ")
		} else {
			return strings.Join(deviceClangGlobalCflags, " ")
		}
	})

	staticVariableExportedToBazel("HostClangGlobalCflags", ClangFilterUnknownCflags(hostGlobalCflags))
	staticVariableExportedToBazel("NoOverrideClangGlobalCflags", append(ClangFilterUnknownCflags(noOverrideGlobalCflags), "${ClangExtraNoOverrideCflags}"))
	staticVariableExportedToBazel("CommonClangGlobalCppflags", append(ClangFilterUnknownCflags(commonGlobalCppflags), "${ClangExtraCppflags}"))
	staticVariableExportedToBazel("ClangExternalCflags", []string{"${ClangExtraExternalCflags}"})

	// Everything in these lists is a crime against abstraction and dependency tracking.
	// Do not add anything to this list.
	pctx.PrefixedExistentPathsForSourcesVariable("CommonGlobalIncludes", "-I",
		[]string{
			"system/core/include",
			"system/logging/liblog/include",
			"system/media/audio/include",
			"hardware/libhardware/include",
			"hardware/libhardware_legacy/include",
			"hardware/ril/include",
			"frameworks/native/include",
			"frameworks/native/opengl/include",
			"frameworks/av/include",
		})

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

	pctx.StaticVariableWithEnvOverride("RECXXPool", "RBE_CXX_POOL", remoteexec.DefaultPool)
	pctx.StaticVariableWithEnvOverride("RECXXLinksPool", "RBE_CXX_LINKS_POOL", remoteexec.DefaultPool)
	pctx.StaticVariableWithEnvOverride("REClangTidyPool", "RBE_CLANG_TIDY_POOL", remoteexec.DefaultPool)
	pctx.StaticVariableWithEnvOverride("RECXXLinksExecStrategy", "RBE_CXX_LINKS_EXEC_STRATEGY", remoteexec.LocalExecStrategy)
	pctx.StaticVariableWithEnvOverride("REClangTidyExecStrategy", "RBE_CLANG_TIDY_EXEC_STRATEGY", remoteexec.LocalExecStrategy)
	pctx.StaticVariableWithEnvOverride("REAbiDumperExecStrategy", "RBE_ABI_DUMPER_EXEC_STRATEGY", remoteexec.LocalExecStrategy)
	pctx.StaticVariableWithEnvOverride("REAbiLinkerExecStrategy", "RBE_ABI_LINKER_EXEC_STRATEGY", remoteexec.LocalExecStrategy)
}

func setSdclangVars() {
	sdclangPath := ""
	sdclangAEFlag := ""
	sdclangFlags := ""

	product := os.Getenv("TARGET_BOARD_PLATFORM")
	aeConfigPath := os.Getenv("SDCLANG_AE_CONFIG")
	sdclangConfigPath := os.Getenv("SDCLANG_CONFIG")
	sdclangSA := os.Getenv("SDCLANG_SA_ENABLED")

	// Bail out if SDCLANG_CONFIG isn't set
	if sdclangConfigPath == "" {
		return
	}

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
			if b {
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
	if sdclang := os.Getenv("SDCLANG"); sdclang != "" {
		if override, err := strconv.ParseBool(sdclang); err == nil {
			SDClang = override
		}
	}

	// Sanity check SDCLANG_PATH
	if envPath := os.Getenv("SDCLANG_PATH"); SDClang && sdclangPath == "" && envPath == "" {
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

func envOverrideFunc(envVar, defaultVal string) func(ctx android.PackageVarContext) string {
	return func(ctx android.PackageVarContext) string {
		if override := ctx.Config().Getenv(envVar); override != "" {
			return override
		}
		return defaultVal
	}
}
