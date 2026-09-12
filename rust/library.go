// Copyright 2019 The Android Open Source Project
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

package rust

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/google/blueprint"
	"github.com/google/blueprint/depset"
	"github.com/google/blueprint/pathtools"
	"github.com/google/blueprint/proptools"

	"android/soong/android"
	"android/soong/cc"
	cc_config "android/soong/cc/config"
	"android/soong/rust/config"
)

var (
	RlibStdlibSuffix = ".rlib-std"
	CoreStdlibSuffix = ".rlib-core"
)

func init() {
	android.RegisterModuleType("rust_library", RustLibraryFactory)
	android.RegisterModuleType("rust_library_dylib", RustLibraryDylibFactory)
	android.RegisterModuleType("rust_library_rlib", RustLibraryRlibFactory)
	android.RegisterModuleType("rust_library_host", RustLibraryHostFactory)
	android.RegisterModuleType("rust_library_host_dylib", RustLibraryDylibHostFactory)
	android.RegisterModuleType("rust_library_host_rlib", RustLibraryRlibHostFactory)
	android.RegisterModuleType("rust_ffi", RustFFIFactory)
	android.RegisterModuleType("rust_ffi_shared", RustFFISharedFactory)
	android.RegisterModuleType("rust_ffi_host", RustFFIHostFactory)
	android.RegisterModuleType("rust_ffi_host_shared", RustFFISharedHostFactory)
	android.RegisterModuleType("rust_ffi_static", RustLibraryRlibFactory)
	android.RegisterModuleType("rust_ffi_host_static", RustLibraryRlibHostFactory)
}

type VariantLibraryProperties struct {
	Enabled           *bool                            `android:"arch_variant"`
	Srcs              proptools.Configurable[[]string] `android:"path,arch_variant"`
	Features          proptools.Configurable[[]string] `android:"arch_variant"`
	Rustlibs          proptools.Configurable[[]string] `android:"arch_variant"`
	Static_libs       proptools.Configurable[[]string] `android:"arch_variant"`
	Whole_static_libs proptools.Configurable[[]string] `android:"arch_variant"`
	Shared_libs       proptools.Configurable[[]string] `android:"arch_variant"`
	Cfgs              proptools.Configurable[[]string] `android:"arch_variant"`
	Flags             []string                         `android:"arch_variant"`
}

type LibraryCompilerProperties struct {
	Rlib   VariantLibraryProperties  `android:"arch_variant"`
	Dylib  VariantLibraryProperties  `android:"arch_variant"`
	Shared VariantLibraryProperties  `android:"arch_variant"`
	Static VariantLibraryProperties  `android:"arch_variant"`
	No_std *VariantLibraryProperties `android:"arch_variant"`

	// TODO: Remove this when all instances of Include_dirs have been removed from rust_ffi modules.
	// path to include directories to pass to cc_* modules, only relevant for static/shared variants (deprecated, use export_include_dirs instead).
	Include_dirs []string `android:"path,arch_variant"`

	// path to include directories to export to cc_* modules, only relevant for static/shared variants.
	Export_include_dirs []string `android:"path,arch_variant"`

	// Version script to pass to the linker. By default this will replace the
	// implicit rustc emitted version script to mirror expected behavior in CC.
	// This is only relevant for rust_ffi_shared modules which are exposing a
	// versioned C API.
	Version_script *string `android:"path,arch_variant"`

	// A version_script formatted text file with additional symbols to export
	// for rust shared or dylibs which the rustc compiler does not automatically
	// export, e.g. additional symbols from whole_static_libs. Unlike
	// Version_script, this is not meant to imply a stable API.
	Extra_exported_symbols *string `android:"path,arch_variant"`

	// Whether this library is part of the Rust toolchain sysroot.
	Sysroot *bool

	// Deprecated - exclude this rust_ffi target from being included in APEXes.
	// TODO(b/362509506): remove this once all apex_exclude uses are switched to stubs.
	Apex_exclude *bool

	// Generate stubs to make this library accessible to APEXes.
	// Can only be set for modules producing shared libraries.
	Stubs cc.StubsProperties `android:"arch_variant"`
}

type LibraryMutatedProperties struct {
	// Build a dylib variant
	BuildDylib bool `blueprint:"mutated"`
	// Build an rlib variant
	BuildRlib bool `blueprint:"mutated"`
	// Build a shared library variant
	BuildShared bool `blueprint:"mutated"`
	// Build a static library variant
	BuildStatic bool `blueprint:"mutated"`

	// This variant is a dylib
	VariantIsDylib bool `blueprint:"mutated"`
	// This variant is an rlib
	VariantIsRlib bool `blueprint:"mutated"`
	// This variant is a shared library
	VariantIsShared bool `blueprint:"mutated"`
	// This variant is a source provider
	VariantIsSource bool `blueprint:"mutated"`
	// This variant is no_std
	VariantIsNoStd bool `blueprint:"mutated"`

	// This variant is disabled and should not be compiled
	// (used for SourceProvider variants that produce only source)
	VariantIsDisabled bool `blueprint:"mutated"`

	// Whether this library variant should be link libstd via rlibs
	VariantIsStaticStd bool `blueprint:"mutated"`

	// This variant is a stubs lib
	BuildStubs bool `blueprint:"mutated"`
	// This variant is the latest version
	IsLatestVersion bool `blueprint:"mutated"`
	// Version of the stubs lib
	StubsVersion string `blueprint:"mutated"`
	// List of all stubs versions associated with an implementation lib
	AllStubsVersions []string `blueprint:"mutated"`
}

type libraryDecorator struct {
	*baseCompiler
	*flagExporter
	stripper Stripper

	Properties        LibraryCompilerProperties
	MutatedProperties LibraryMutatedProperties
	includeDirs       android.Paths
	sourceProvider    SourceProvider

	// table-of-contents file for cdylib crates to optimize out relinking when possible
	tocFile android.OptionalPath

	// Path to the file containing the APIs exported by this library
	stubsSymbolFilePath    android.Path
	apiListCoverageXmlPath android.ModuleOutPath
	versionScriptPath      android.OptionalPath
}

func (library *libraryDecorator) stubs() bool {
	return library.MutatedProperties.BuildStubs
}

func (library *libraryDecorator) setAPIListCoverageXMLPath(xml android.ModuleOutPath) {
	library.apiListCoverageXmlPath = xml
}

func (library *libraryDecorator) libraryProperties() LibraryCompilerProperties {
	return library.Properties
}

type libraryInterface interface {
	cc.VersionedInterface

	rlib() bool
	dylib() bool
	static() bool
	shared() bool
	sysroot() bool
	source() bool
	apexExclude() bool

	// Returns true if the build options for the module have selected a particular build type
	buildRlib() bool
	buildDylib() bool
	buildShared() bool
	buildStatic() bool
	buildNoStd() bool

	// Sets a particular variant type
	setRlib()
	setDylib()
	setNoStd()
	setShared()
	setStatic()
	setSource()

	// libstd linkage functions
	rlibStd() bool
	setRlibStd()
	setDylibStd()

	// Build a specific library variant
	BuildOnlyFFI()
	BuildOnlyRust()
	BuildOnlyRlib()
	BuildOnlyDylib()
	BuildOnlyStatic()
	BuildOnlyShared()

	toc() android.OptionalPath

	IsStubsImplementationRequired() bool
	setAPIListCoverageXMLPath(out android.ModuleOutPath)

	libraryProperties() LibraryCompilerProperties
}

func (library *libraryDecorator) setSysroot() {
	library.Properties.Sysroot = proptools.BoolPtr(true)
}

func (library *libraryDecorator) nativeCoverage() bool {
	return !library.BuildStubs()
}

func (library *libraryDecorator) toc() android.OptionalPath {
	return library.tocFile
}

func (library *libraryDecorator) rlib() bool {
	return library.MutatedProperties.VariantIsRlib
}

func (library *libraryDecorator) sysroot() bool {
	return Bool(library.Properties.Sysroot)
}

func (library *libraryDecorator) dylib() bool {
	return library.MutatedProperties.VariantIsDylib
}

func (library *libraryDecorator) shared() bool {
	return library.MutatedProperties.VariantIsShared
}

func (library *libraryDecorator) noStd() bool {
	return library.MutatedProperties.VariantIsNoStd
}

func (library *libraryDecorator) static() bool {
	return false
}

func (library *libraryDecorator) source() bool {
	return library.MutatedProperties.VariantIsSource
}

func (library *libraryDecorator) apexExclude() bool {
	return Bool(library.Properties.Apex_exclude)
}

func (library *libraryDecorator) buildRlib() bool {
	return library.MutatedProperties.BuildRlib && BoolDefault(library.Properties.Rlib.Enabled, true)
}

func (library *libraryDecorator) buildDylib() bool {
	return library.MutatedProperties.BuildDylib && BoolDefault(library.Properties.Dylib.Enabled, true)
}

func (library *libraryDecorator) buildShared() bool {
	return library.MutatedProperties.BuildShared && BoolDefault(library.Properties.Shared.Enabled, true)
}

func (library *libraryDecorator) buildStatic() bool {
	return library.MutatedProperties.BuildStatic && BoolDefault(library.Properties.Static.Enabled, true)
}

func (library *libraryDecorator) buildNoStd() bool {
	return library.Properties.No_std != nil && BoolDefault(library.Properties.No_std.Enabled, false)
}

func (library *libraryDecorator) setRlib() {
	library.MutatedProperties.VariantIsRlib = true
	library.MutatedProperties.VariantIsDylib = false
	library.MutatedProperties.VariantIsShared = false
	library.MutatedProperties.VariantIsNoStd = library.noStdlibs()
}

func (library *libraryDecorator) setDylib() {
	library.MutatedProperties.VariantIsRlib = false
	library.MutatedProperties.VariantIsDylib = true
	library.MutatedProperties.VariantIsShared = false
	library.MutatedProperties.VariantIsNoStd = false
}

func (library *libraryDecorator) rlibStd() bool {
	return library.MutatedProperties.VariantIsStaticStd
}

func (library *libraryDecorator) setRlibStd() {
	library.forceStdlibs()
	library.MutatedProperties.VariantIsNoStd = false
	library.MutatedProperties.VariantIsStaticStd = true
}

func (library *libraryDecorator) setNoStd() {
	library.setNoStdlibs()
	library.MutatedProperties.VariantIsNoStd = true
}

func (library *libraryDecorator) setDylibStd() {
	library.forceStdlibs()
	library.MutatedProperties.VariantIsNoStd = false
	library.MutatedProperties.VariantIsStaticStd = false
}

func (library *libraryDecorator) setShared() {
	library.MutatedProperties.VariantIsShared = true
	library.MutatedProperties.VariantIsRlib = false
	library.MutatedProperties.VariantIsDylib = false
	library.MutatedProperties.VariantIsNoStd = library.noStdlibs()
}

func (library *libraryDecorator) setStatic() {
	panic(fmt.Errorf("static variant is not supported for rust modules, use the rlib variant instead"))
}

func (library *libraryDecorator) setSource() {
	library.MutatedProperties.VariantIsSource = true
}

func (library *libraryDecorator) autoDep(ctx android.BottomUpMutatorContext) autoDep {
	if library.preferRlib() {
		return rlibAutoDep
	} else if library.rlib() || library.static() {
		return rlibAutoDep
	} else if library.dylib() || library.shared() {
		return dylibAutoDep
	} else {
		panic(fmt.Errorf("autoDep called on library %q that has no enabled variants", ctx.ModuleName()))
	}
}

func (library *libraryDecorator) stdLinkage(device bool) StdLinkage {
	if library.sysroot() {
		return NoCore
	} else if library.noStd() || library.noStdlibs() {
		return RlibCore
	} else if library.static() || library.MutatedProperties.VariantIsStaticStd {
		return RlibStd
	} else {
		return library.baseCompiler.stdLinkage(device)
	}
}

var _ compiler = (*libraryDecorator)(nil)
var _ libraryInterface = (*libraryDecorator)(nil)
var _ cc.VersionedInterface = (*libraryDecorator)(nil)
var _ exportedFlagsProducer = (*libraryDecorator)(nil)
var _ cc.VersionedInterface = (*libraryDecorator)(nil)

func (library *libraryDecorator) HasLLNDKStubs() bool {
	// Rust LLNDK is currently unsupported
	return false
}

func (library *libraryDecorator) HasVendorPublicLibrary() bool {
	// Rust does not support vendor_public_library yet.
	return false
}

func (library *libraryDecorator) HasLLNDKHeaders() bool {
	// Rust LLNDK is currently unsupported
	return false
}

func (library *libraryDecorator) HasStubsVariants() bool {
	// Just having stubs.symbol_file is enough to create a stub variant. In that case
	// the stub for the future API level is created.
	return library.Properties.Stubs.Symbol_file != nil ||
		len(library.Properties.Stubs.Versions) > 0
}

func (library *libraryDecorator) IsStubsImplementationRequired() bool {
	return BoolDefault(library.Properties.Stubs.Implementation_installable, true)
}

func (library *libraryDecorator) GetAPIListCoverageXMLPath() android.ModuleOutPath {
	return library.apiListCoverageXmlPath
}

func (library *libraryDecorator) AllStubsVersions() []string {
	return library.MutatedProperties.AllStubsVersions
}

func (library *libraryDecorator) SetAllStubsVersions(versions []string) {
	library.MutatedProperties.AllStubsVersions = versions
}

func (library *libraryDecorator) SetStubsVersion(version string) {
	library.MutatedProperties.StubsVersion = version
}

func (library *libraryDecorator) SetBuildStubs(isLatest bool) {
	library.MutatedProperties.BuildStubs = true
	library.MutatedProperties.IsLatestVersion = isLatest
}

func (library *libraryDecorator) BuildStubs() bool {
	return library.MutatedProperties.BuildStubs
}

func (library *libraryDecorator) ImplementationModuleName(name string) string {
	return name
}

func (library *libraryDecorator) IsLLNDKMovedToApex() bool {
	// Rust does not support LLNDK.
	return false
}

func (library *libraryDecorator) StubsVersion() string {
	return library.MutatedProperties.StubsVersion
}

// stubsVersions implements cc.VersionedInterface.
func (library *libraryDecorator) StubsVersions(ctx android.BaseModuleContext) []string {
	if !library.HasStubsVariants() {
		return nil
	}

	// Future API level is implicitly added if there isn't
	versions := cc.AddCurrentVersionIfNotPresent(library.Properties.Stubs.Versions)
	cc.NormalizeVersions(ctx, versions)
	return versions
}

// rust_library produces all Rust variants (rust_library_dylib and
// rust_library_rlib).
func RustLibraryFactory() android.Module {
	module, library := NewRustLibrary(android.HostAndDeviceSupported)
	library.BuildOnlyRust()
	return module.Init()
}

// rust_ffi produces all FFI variants (rust_ffi_shared, rust_ffi_static).
func RustFFIFactory() android.Module {
	module, library := NewRustLibrary(android.HostAndDeviceSupported)
	library.BuildOnlyFFI()
	return module.Init()
}

// rust_library_dylib produces a Rust dylib (Rust crate type "dylib").
func RustLibraryDylibFactory() android.Module {
	module, library := NewRustLibrary(android.HostAndDeviceSupported)
	library.BuildOnlyDylib()
	return module.Init()
}

// rust_library_rlib and rust_ffi_static produces an rlib (Rust crate type "rlib").
func RustLibraryRlibFactory() android.Module {
	module, library := NewRustLibrary(android.HostAndDeviceSupported)
	library.BuildOnlyRlib()
	return module.Init()
}

// rust_ffi_shared produces a shared library (Rust crate type
// "cdylib").
func RustFFISharedFactory() android.Module {
	module, library := NewRustLibrary(android.HostAndDeviceSupported)
	library.BuildOnlyShared()
	return module.Init()
}

// rust_library_host produces all Rust variants for the host
// (rust_library_dylib_host and rust_library_rlib_host).
func RustLibraryHostFactory() android.Module {
	module, library := NewRustLibrary(android.HostSupported)
	library.BuildOnlyRust()
	return module.Init()
}

// rust_ffi_host produces all FFI variants for the host
// (rust_ffi_static_host and rust_ffi_shared_host).
func RustFFIHostFactory() android.Module {
	module, library := NewRustLibrary(android.HostSupported)
	library.BuildOnlyFFI()
	return module.Init()
}

// rust_library_dylib_host produces a dylib for the host (Rust crate
// type "dylib").
func RustLibraryDylibHostFactory() android.Module {
	module, library := NewRustLibrary(android.HostSupported)
	library.BuildOnlyDylib()
	return module.Init()
}

// rust_library_rlib_host and rust_ffi_static_host produces an rlib for the host
// (Rust crate type "rlib").
func RustLibraryRlibHostFactory() android.Module {
	module, library := NewRustLibrary(android.HostSupported)
	library.BuildOnlyRlib()
	return module.Init()
}

// rust_ffi_shared_host produces an shared library for the host (Rust
// crate type "cdylib").
func RustFFISharedHostFactory() android.Module {
	module, library := NewRustLibrary(android.HostSupported)
	library.BuildOnlyShared()
	return module.Init()
}

func CheckRustLibraryProperties(mctx android.DefaultableHookContext) {
	lib := mctx.Module().(*Module).compiler.(libraryInterface)
	if !lib.buildShared() {
		if lib.libraryProperties().Stubs.Symbol_file != nil ||
			lib.libraryProperties().Stubs.Implementation_installable != nil ||
			len(lib.libraryProperties().Stubs.Versions) > 0 {

			mctx.PropertyErrorf("stubs", "stubs properties can only be set for rust_ffi or rust_ffi_shared modules")
		}
	}
}

func (library *libraryDecorator) BuildOnlyFFI() {
	library.MutatedProperties.BuildDylib = false
	// we build rlibs for later static ffi linkage.
	library.MutatedProperties.BuildRlib = true
	library.MutatedProperties.BuildShared = true
	library.MutatedProperties.BuildStatic = false
}

func (library *libraryDecorator) BuildOnlyRust() {
	library.MutatedProperties.BuildDylib = true
	library.MutatedProperties.BuildRlib = true
	library.MutatedProperties.BuildShared = false
	library.MutatedProperties.BuildStatic = false
}

func (library *libraryDecorator) BuildOnlyDylib() {
	library.MutatedProperties.BuildDylib = true
	library.MutatedProperties.BuildRlib = false
	library.MutatedProperties.BuildShared = false
	library.MutatedProperties.BuildStatic = false
}

func (library *libraryDecorator) BuildOnlyRlib() {
	library.MutatedProperties.BuildDylib = false
	library.MutatedProperties.BuildRlib = true
	library.MutatedProperties.BuildShared = false
	library.MutatedProperties.BuildStatic = false
}

func (library *libraryDecorator) BuildOnlyStatic() {
	library.MutatedProperties.BuildRlib = false
	library.MutatedProperties.BuildDylib = false
	library.MutatedProperties.BuildShared = false
	library.MutatedProperties.BuildStatic = true
}

func (library *libraryDecorator) BuildOnlyShared() {
	library.MutatedProperties.BuildRlib = false
	library.MutatedProperties.BuildDylib = false
	library.MutatedProperties.BuildStatic = false
	library.MutatedProperties.BuildShared = true
}

func NewRustLibrary(hod android.HostOrDeviceSupported) (*Module, *libraryDecorator) {
	module := newModule(hod, android.MultilibBoth)

	library := &libraryDecorator{
		MutatedProperties: LibraryMutatedProperties{
			BuildDylib:  false,
			BuildRlib:   false,
			BuildShared: false,
			BuildStatic: false,
		},
		baseCompiler: NewBaseCompiler("lib", "lib64", InstallInSystem),
		flagExporter: NewFlagExporter(),
	}

	module.compiler = library

	module.SetDefaultableHook(CheckRustLibraryProperties)
	return module, library
}

func (library *libraryDecorator) compilerProps() []any {
	return append(library.baseCompiler.compilerProps(),
		&library.Properties,
		&library.MutatedProperties,
		&library.stripper.StripProperties)
}

func (library *libraryDecorator) compilerDeps(ctx DepsContext, deps Deps) Deps {
	deps = library.baseCompiler.compilerDeps(ctx, deps)

	if library.dylib() || library.shared() {
		if ctx.toolchain().Bionic() {
			deps = bionicDeps(ctx, deps, false)
			deps.CrtBegin = []string{"crtbegin_so"}
			deps.CrtEnd = []string{"crtend_so"}
		} else if ctx.Os() == android.LinuxMusl {
			deps = muslDeps(ctx, deps, false)
			deps.CrtBegin = []string{"libc_musl_crtbegin_so"}
			deps.CrtEnd = []string{"libc_musl_crtend_so"}
		}
	}

	return deps
}

func (library *libraryDecorator) sharedLibFilename(ctx ModuleContext) string {
	return library.getStem(ctx) + ctx.toolchain().SharedLibSuffix()
}

// Library cfg flags common to all variants
func CommonLibraryCfgFlags(ctx android.ModuleContext, flags Flags) Flags {
	return flags
}

func (library *libraryDecorator) cfgFlags(ctx ModuleContext, flags Flags) Flags {
	flags = library.baseCompiler.cfgFlags(ctx, flags)
	flags = CommonLibraryCfgFlags(ctx, flags)

	cfgs := library.baseCompiler.Properties.Cfgs.GetOrDefault(ctx, nil)

	cfgFlags := cfgsToFlags(cfgs)

	flags.RustFlags = append(flags.RustFlags, cfgFlags...)
	flags.RustdocFlags = append(flags.RustdocFlags, cfgFlags...)

	return flags
}

// Common flags applied to all libraries irrespective of properties or variant should be included here
func CommonLibraryCompilerFlags(ctx android.ModuleContext, flags Flags) Flags {
	flags.RustFlags = append(flags.RustFlags, "-C metadata="+ctx.ModuleName())
	if mod, ok := ctx.Module().(*Module); ok {
		if lib, ok := mod.compiler.(*libraryDecorator); ok {
			flags.RustFlags = append(flags.RustFlags, "-C metadata="+lib.stdLinkage(ctx.Device()).variationName())
		}
	}

	return flags
}

func (library *libraryDecorator) compilerFlags(ctx ModuleContext, flags Flags) Flags {
	flags = library.baseCompiler.compilerFlags(ctx, flags)

	flags = CommonLibraryCompilerFlags(ctx, flags)

	if library.rlib() || library.shared() {
		// rlibs collect include dirs as well since they are used to
		// produce staticlibs in the final C linkages
		library.includeDirs = append(library.includeDirs, android.PathsForModuleSrc(ctx, library.Properties.Include_dirs)...)
		library.includeDirs = append(library.includeDirs, android.PathsForModuleSrc(ctx, library.Properties.Export_include_dirs)...)
	}

	if library.shared() {
		if ctx.Darwin() {
			flags.LinkFlags = flags.LinkFlags.AppendNoDeps(
				"-dynamic_lib",
				"-install_name @rpath/"+library.sharedLibFilename(ctx),
			)
		} else {
			if !ctx.Windows() {
				flags.LinkFlags = flags.LinkFlags.AppendNoDeps("-Wl,-soname=" + library.sharedLibFilename(ctx))
			}
		}
	}

	return flags
}

func (library *libraryDecorator) compile(ctx ModuleContext, flags Flags, deps PathDeps) buildOutput {
	var outputFile android.ModuleOutPath
	var ret buildOutput
	var fileName string
	crateRootPath := library.crateRootPath(ctx)

	deps.SrcFiles = append(deps.SrcFiles, crateRootPath)
	deps.SrcFiles = append(deps.SrcFiles, library.crateSources(ctx)...)

	if library.sourceProvider != nil {
		deps.srcProviderFiles = append(deps.srcProviderFiles, library.sourceProvider.Srcs()...)
	}

	// Ensure link dirs are not duplicated
	deps.linkDirs = android.FirstUniqueStrings(deps.linkDirs)
	deps.linkDirsDeps = android.FirstUniquePaths(deps.linkDirsDeps)

	// Calculate output filename
	if library.rlib() {
		fileName = library.getStem(ctx) + ctx.toolchain().RlibSuffix()
		outputFile = android.PathForModuleOut(ctx, fileName)
		ret.outputFile = outputFile
	} else if library.dylib() {
		fileName = library.getStem(ctx) + ctx.toolchain().DylibSuffix()
		outputFile = android.PathForModuleOut(ctx, fileName)
		ret.outputFile = outputFile
	} else if library.static() {
		fileName = library.getStem(ctx) + ctx.toolchain().StaticLibSuffix()
		outputFile = android.PathForModuleOut(ctx, fileName)
		ret.outputFile = outputFile
	} else if library.shared() {
		fileName = library.sharedLibFilename(ctx)
		outputFile = android.PathForModuleOut(ctx, fileName)
		ret.outputFile = outputFile
	}

	if !library.rlib() && !library.static() && library.stripper.NeedsStrip(ctx) {
		strippedOutputFile := outputFile
		outputFile = android.PathForModuleOut(ctx, "unstripped", fileName)
		library.stripper.StripExecutableOrSharedLib(ctx, outputFile, strippedOutputFile)

		library.strippedOutputFile = android.OptionalPathForPath(strippedOutputFile)
	}
	library.unstrippedOutputFile = outputFile
	checkJsonFile := android.PathForModuleOut(ctx, outputFile.Base()+".checkJson")
	library.checkJsonFile = android.OptionalPathForPath(checkJsonFile)

	flags.RustFlags = append(flags.RustFlags, deps.depFlags...)
	flags.LinkFlags = flags.LinkFlags.AppendNoDeps(deps.depLinkFlags...)
	flags.LinkFlags = flags.LinkFlags.AppendNoDeps(deps.rustLibObjects...)
	flags.LinkFlags = flags.LinkFlags.AppendNoDeps(deps.staticLibObjects...)
	flags.LinkFlags = flags.LinkFlags.AppendNoDeps(deps.wholeStaticLibObjects...)

	if ctx.Windows() {
		for _, lib := range deps.sharedLibObjects {
			// Windows uses the .lib import library at link-time and at runtime
			// uses the .dll library, so we need to make sure we're passing the
			// import library to the linker.
			flags.LinkFlags = flags.LinkFlags.AppendNoDeps(pathtools.ReplaceExtension(lib, "lib"))
		}
	} else {
		flags.LinkFlags = flags.LinkFlags.AppendNoDeps(deps.sharedLibObjects...)
	}

	if String(library.Properties.Version_script) != "" {
		if String(library.Properties.Extra_exported_symbols) != "" {
			ctx.ModuleErrorf("version_script and extra_exported_symbols cannot both be set.")
		}

		if library.shared() {
			// "-Wl,--android-version-script" signals to the rustcLinker script
			// that the default version script should be removed.
			flags.LinkerScriptFlags = append(flags.LinkerScriptFlags,
				"-Wl,--android-version-script="+android.PathForModuleSrc(ctx, String(library.Properties.Version_script)).String())
			deps.LinkerDeps = append(deps.LinkerDeps, android.PathForModuleSrc(ctx, String(library.Properties.Version_script)))
		} else if !library.static() && !library.rlib() {
			// We include rlibs here because rust_ffi produces rlib variants
			ctx.PropertyErrorf("version_script", "can only be set for rust_ffi modules")
		}
	}

	if String(library.Properties.Extra_exported_symbols) != "" {
		// Passing a second version script (rustc calculates and emits a
		// default version script) will concatenate the first version script.
		versionScript := android.PathForModuleSrc(ctx, String(library.Properties.Extra_exported_symbols))
		newFlags := cc_config.FlagsWithDeps{
			Flags: "-Wl,--version-script=" + versionScript.String(),
			Deps:  []android.Path{versionScript},
		}
		flags.LinkFlags = flags.LinkFlags.Append(newFlags)
	}

	if library.dylib() {

		// We need prefer-dynamic for now to avoid linking in the static stdlib. See:
		// https://github.com/rust-lang/rust/issues/19680
		// https://github.com/rust-lang/rust/issues/34909
		flags.RustFlags = append(flags.RustFlags, "-C prefer-dynamic")
	}

	// Call the appropriate builder for this library type
	if library.stubs() {
		ccFlags := library.getApiStubsCcFlags(ctx)
		stubObjs := library.compileModuleLibApiStubs(ctx, ccFlags)
		cc.BuildRustStubs(ctx, outputFile, stubObjs, ccFlags)
	} else if library.rlib() {
		ret.kytheFile = TransformSrctoRlib(ctx, crateRootPath, deps, flags, outputFile, checkJsonFile).kytheFile
	} else if library.dylib() {
		ret.kytheFile = TransformSrctoDylib(ctx, crateRootPath, deps, flags, outputFile, checkJsonFile).kytheFile
	} else if library.static() {
		ret.kytheFile = TransformSrctoStatic(ctx, crateRootPath, deps, flags, outputFile, checkJsonFile).kytheFile
	} else if library.shared() {
		ret.kytheFile = TransformSrctoShared(ctx, crateRootPath, deps, flags, outputFile, checkJsonFile).kytheFile
	}

	// rlibs and dylibs propagate their shared, whole static, and rustlib dependencies
	if library.rlib() || library.dylib() {
		library.exportLinkDirs(deps.linkDirs, deps.linkDirsDeps)
		library.exportRustLibs(deps.rustLibObjects...)
		library.exportSharedLibs(deps.sharedLibObjects...)
		library.exportWholeStaticLibs(deps.wholeStaticLibObjects...)
	}

	// rlibs also propagate their staticlibs dependencies
	if library.rlib() {
		library.exportStaticLibs(deps.staticLibObjects...)
	}
	// Since we have FFI rlibs, we need to collect their includes as well
	if library.static() || library.shared() || library.rlib() || library.stubs() {
		ccExporter := cc.FlagExporterInfo{
			IncludeDirs: android.FirstUniquePaths(library.includeDirs),
		}
		if library.rlib() {
			ccExporter.RustRlibDeps = append(ccExporter.RustRlibDeps, deps.reexportedCcRlibDeps...)
			ccExporter.RustRlibDeps = append(ccExporter.RustRlibDeps, deps.reexportedWholeCcRlibDeps...)
		}
		android.SetProvider(ctx, cc.FlagExporterInfoProvider, ccExporter)
	}

	if library.dylib() {
		// reexport whole-static'd dependencies for dylibs.
		library.wholeRustRlibDeps = append(library.wholeRustRlibDeps, deps.reexportedWholeCcRlibDeps...)
	}

	if library.shared() || library.stubs() {
		// Optimize out relinking against shared libraries whose interface hasn't changed by
		// depending on a table of contents file instead of the library itself.
		tocFile := outputFile.ReplaceExtension(ctx, flags.Toolchain.SharedLibSuffix()[1:]+".toc")
		library.tocFile = android.OptionalPathForPath(tocFile)
		cc.TransformSharedObjectToToc(ctx, outputFile, tocFile)

		android.SetProvider(ctx, cc.SharedLibraryInfoProvider, cc.SharedLibraryInfo{
			TableOfContents: android.OptionalPathForPath(tocFile),
			SharedLibrary:   outputFile,
			Target:          ctx.Target(),
			IsStubs:         library.BuildStubs(),
		})
	}

	if library.static() {
		depSet := depset.NewBuilder[android.Path](depset.TOPOLOGICAL).Direct(outputFile).Build()
		android.SetProvider(ctx, cc.StaticLibraryInfoProvider, cc.StaticLibraryInfo{
			StaticLibrary: outputFile,

			TransitiveStaticLibrariesForOrdering: depSet,
		})
	}
	cc.AddStubDependencyProviders(ctx)

	// Set our flagexporter provider to export relevant Rust flags
	library.setRustProvider(ctx)

	return ret
}

func (library *libraryDecorator) crateRootPath(ctx ModuleContext) android.Path {
	if library.sourceProvider != nil {
		srcs := library.sourceProvider.Srcs()
		if len(srcs) == 0 {
			ctx.PropertyErrorf("srcs", "Source provider generated 0 sources")
			return nil
		}
		// Assume the first source from the source provider is the library entry point.
		return srcs[0]
	} else {
		return library.baseCompiler.crateRootPath(ctx)
	}
}

func (library *libraryDecorator) getApiStubsCcFlags(ctx ModuleContext) cc.Flags {
	ccFlags := cc.Flags{}
	toolchain := cc_config.FindToolchain(ctx.Os(), ctx.Arch(), false)

	platformSdkVersion := ""
	if ctx.Device() {
		platformSdkVersion = ctx.Config().PlatformSdkVersion().String()
	}
	minSdkVersion := cc.MinSdkVersion(ctx, ctx.RustModule(), cc.CtxIsForPlatform(ctx), ctx.Device(), platformSdkVersion)

	// Collect common CC compilation flags
	ccFlags = cc.CommonLinkerFlags(ctx, ccFlags, toolchain, false)
	ccFlags = cc.CommonLibraryLinkerFlags(ctx, ccFlags, toolchain, library.getStem(ctx))
	ccFlags = cc.AddStubLibraryCompilerFlags(ccFlags)
	ccFlags = cc.AddTargetFlags(ctx, ccFlags, toolchain, minSdkVersion, false, false)
	ccFlags = cc.AddStubLibraryLinkerFlags(ctx, ccFlags)

	return ccFlags
}

func (library *libraryDecorator) compileModuleLibApiStubs(ctx ModuleContext, ccFlags cc.Flags) cc.Objects {
	mod := ctx.RustModule()

	symbolFile := String(library.Properties.Stubs.Symbol_file)
	library.stubsSymbolFilePath = android.PathForModuleSrc(ctx, symbolFile)

	apiParams := cc.ApiStubsParams{
		NotInPlatform:  mod.NotInPlatform(),
		IsNdk:          mod.IsNdk(ctx.Config()),
		BaseModuleName: mod.BaseModuleName(),
		ModuleName:     ctx.ModuleName(),
	}
	flag := cc.GetApiStubsFlags(ctx, apiParams)

	nativeAbiResult := cc.ParseNativeAbiDefinition(ctx, symbolFile,
		android.ApiLevelOrPanic(ctx, library.MutatedProperties.StubsVersion), flag)
	objs := cc.CompileStubLibrary(ctx, ccFlags, nativeAbiResult.StubSrc, mod.getSharedFlags())

	library.versionScriptPath = android.OptionalPathForPath(nativeAbiResult.VersionScript)

	// Parse symbol file to get API list for coverage
	if library.StubsVersion() == "current" && ctx.PrimaryArch() && !mod.InRecovery() && !mod.InProduct() && !mod.InVendor() {
		library.apiListCoverageXmlPath = cc.ParseSymbolFileForAPICoverage(ctx, symbolFile)
	}

	return objs
}

func (library *libraryDecorator) rustdoc(ctx ModuleContext, flags Flags,
	deps PathDeps) android.OptionalPath {
	// rustdoc has builtin support for documenting config specific information
	// regardless of the actual config it was given
	// (https://doc.rust-lang.org/rustdoc/advanced-features.html#cfgdoc-documenting-platform-specific-or-feature-specific-information),
	// so we generate the rustdoc for only the primary module so that we have a
	// single set of docs to refer to.
	if !ctx.IsPrimaryModule() {
		return android.OptionalPath{}
	}

	return android.OptionalPathForPath(Rustdoc(ctx, library.crateRootPath(ctx),
		deps, flags))
}

func (library *libraryDecorator) getStem(ctx ModuleContext) string {
	stem := library.getStemWithoutSuffix(ctx)
	validateLibraryStem(ctx, stem, library.crateName())

	return stem + String(library.baseCompiler.Properties.Suffix)
}

func (library *libraryDecorator) install(ctx ModuleContext) {
	// Only shared and dylib variants make sense to install.
	if library.shared() || library.dylib() {
		library.baseCompiler.install(ctx)
	}
}

func (library *libraryDecorator) Disabled() bool {
	return library.MutatedProperties.VariantIsDisabled
}

func (library *libraryDecorator) SetDisabled() {
	library.MutatedProperties.VariantIsDisabled = true
}

func (library *libraryDecorator) moduleInfoJSON(ctx ModuleContext, moduleInfoJSON *android.ModuleInfoJSON) {
	library.baseCompiler.moduleInfoJSON(ctx, moduleInfoJSON)

	if library.rlib() {
		moduleInfoJSON.Class = []string{"RLIB_LIBRARIES"}
	} else if library.dylib() {
		moduleInfoJSON.Class = []string{"DYLIB_LIBRARIES"}
	} else if library.static() {
		moduleInfoJSON.Class = []string{"STATIC_LIBRARIES"}
	} else if library.shared() {
		moduleInfoJSON.Class = []string{"SHARED_LIBRARIES"}
	}
}

var validCrateName = regexp.MustCompile("[^a-zA-Z0-9_]+")

func validateLibraryStem(ctx BaseModuleContext, filename string, crate_name string) {
	if crate_name == "" {
		ctx.PropertyErrorf("crate_name", "crate_name must be defined.")
	}

	// crate_names are used for the library output file, and rustc expects these
	// to be alphanumeric with underscores allowed.
	if validCrateName.MatchString(crate_name) {
		ctx.PropertyErrorf("crate_name",
			"library crate_names must be alphanumeric with underscores allowed")
	}

	// Libraries are expected to begin with "lib" followed by the crate_name
	if !strings.HasPrefix(filename, "lib"+crate_name) {
		ctx.ModuleErrorf("Invalid name or stem property; library filenames must start with lib<crate_name>")
	}
}

type libraryTransitionMutator struct{}

func (libraryTransitionMutator) split(ctx android.BaseModuleContext) []string {
	m, ok := ctx.Module().(*Module)
	if !ok || m.compiler == nil {
		return []string{""}
	}
	library, ok := m.compiler.(libraryInterface)
	if !ok {
		return []string{""}
	}

	// Don't produce rlib/dylib/source variants for shared or static variants
	if library.shared() || library.static() {
		return []string{""}
	}

	var variants []string
	// The source variant is used for SourceProvider modules. The other variants (i.e. rlib and dylib)
	// depend on this variant. It must be the first variant to be declared.
	if m.sourceProvider != nil {
		variants = append(variants, sourceVariation)
	}
	if library.buildRlib() {
		variants = append(variants, rlibVariation)
	}
	if library.buildDylib() && !ctx.Host() && (!m.compiler.noStdlibs() || library.sysroot()) {
		// Hosts do not produce dylib variants.
		// Libraries which are rlib-core should not have a dylib variant.
		variants = append(variants, dylibVariation)
	}

	if len(variants) == 0 {
		return []string{""}
	}

	return variants
}

func (l libraryTransitionMutator) Split(ctx android.BaseModuleContext) []string {
	allSplits := l.split(ctx)
	if ctx.Config().GetBuildFlagBool("RELEASE_SOONG_RUST_VARIANT_ON_DEMAND") {
		return allSplits[0:1]
	} else {
		return allSplits
	}
}
func (l libraryTransitionMutator) SplitOnDemand(ctx android.BaseModuleContext) []string {
	allSplits := l.split(ctx)
	if len(allSplits) <= 1 || !ctx.Config().GetBuildFlagBool("RELEASE_SOONG_RUST_VARIANT_ON_DEMAND") {
		return nil
	} else {
		return allSplits[1:]
	}
}

func (libraryTransitionMutator) OutgoingTransition(ctx android.OutgoingTransitionContext, sourceVariation string) string {
	if ctx.DepTag() == android.PrebuiltDepTag {
		return sourceVariation
	}
	return ""
}

func (libraryTransitionMutator) IncomingTransition(ctx android.IncomingTransitionContext, incomingVariation string) string {
	m, ok := ctx.Module().(*Module)
	if !ok || m.compiler == nil {
		return ""
	}
	library, ok := m.compiler.(libraryInterface)
	if !ok {
		return ""
	}

	if incomingVariation == "" {
		if m.sourceProvider != nil {
			return sourceVariation
		}
		if library.shared() {
			return ""
		}
		if library.buildRlib() {
			return rlibVariation
		}
		if library.buildDylib() {
			return dylibVariation
		}
	}
	return incomingVariation
}

func (libraryTransitionMutator) Mutate(ctx android.BottomUpMutatorContext, variation string) {
	m, ok := ctx.Module().(*Module)
	if !ok || m.compiler == nil {
		return
	}
	library, ok := m.compiler.(libraryInterface)
	if !ok {
		return
	}

	switch variation {
	case rlibVariation:
		library.setRlib()
	case dylibVariation:
		library.setDylib()
		if m.ModuleBase.ImageVariation().Variation == android.VendorRamdiskVariation {
			// TODO(b/165791368)
			// Disable dylib Vendor Ramdisk variations until we support these.
			m.Disable()
		}

	case sourceVariation:
		library.setSource()
		// The source variant does not produce any library.
		// Disable the compilation steps.
		m.compiler.SetDisabled()
	}

	// If a source variant is created, add an inter-variant dependency
	// between the other variants and the source variant.
	if m.sourceProvider != nil && variation != sourceVariation {
		ctx.AddVariationDependencies(
			[]blueprint.Variation{
				{Mutator: "rust_libraries", Variation: sourceVariation},
			},
			sourceDepTag, ctx.ModuleName())
	}

	if prebuilt, ok := m.compiler.(*prebuiltLibraryDecorator); ok {
		if Bool(prebuilt.Properties.Force_use_prebuilt) && len(prebuilt.prebuiltSrcs(ctx)) > 0 {
			m.Prebuilt().SetUsePrebuilt(true)
		}
	}
}

type libstdTransitionMutator struct{}

func (libstdTransitionMutator) split(ctx android.BaseModuleContext) []string {
	if m, ok := ctx.Module().(*Module); ok && m.compiler != nil && !m.compiler.Disabled() {
		// Only create a variant if a library is actually being built.
		if library, ok := m.compiler.(libraryInterface); ok {
			if ctx.Host() {
				// Host builds don't have libcore available today
				library.setRlibStd()
			}
			if library.sysroot() {
				// Sysroot libraries have a trivial stdlinkage
				library.setNoStd()
				return []string{""}
			}
			if m.compiler.noStdlibs() {
				return []string{"rlib-core"}
			}
			if library.rlib() {
				if ctx.Host() {
					// Hosts do not produce dylib variants, so there's only one std option.
					return []string{"rlib-std"}
				}
				if library.buildNoStd() {
					return []string{"rlib-core", "rlib-std", "dylib-std"}
				} else {
					return []string{"rlib-std", "dylib-std"}
				}
			}
		}
	}
	return []string{""}
}

func (l libstdTransitionMutator) Split(ctx android.BaseModuleContext) []string {
	allSplits := l.split(ctx)
	if ctx.Config().GetBuildFlagBool("RELEASE_SOONG_RUST_VARIANT_ON_DEMAND") {
		return allSplits[0:1]
	} else {
		return allSplits
	}
}

func (l libstdTransitionMutator) SplitOnDemand(ctx android.BaseModuleContext) []string {
	allSplits := l.split(ctx)
	if len(allSplits) <= 1 || !ctx.Config().GetBuildFlagBool("RELEASE_SOONG_RUST_VARIANT_ON_DEMAND") {
		return nil
	} else {
		return allSplits[1:]
	}
}

func (libstdTransitionMutator) OutgoingTransition(ctx android.OutgoingTransitionContext, sourceVariation string) string {
	if ctx.DepTag() == android.PrebuiltDepTag {
		return sourceVariation
	}
	return ""
}

func (libstdTransitionMutator) IncomingTransition(ctx android.IncomingTransitionContext, incomingVariation string) string {
	if m, ok := ctx.Module().(*Module); ok && m.compiler != nil && !m.compiler.Disabled() {
		if library, ok := m.compiler.(libraryInterface); ok {
			if library.shared() {
				return ""
			}
			if library.rlib() && !library.sysroot() {
				if incomingVariation != "" {
					return incomingVariation
				}
				if m.compiler.noStdlibs() || (ctx.Device() && library.buildNoStd()) {
					return "rlib-core"
				} else {
					return "rlib-std"
				}
			}
		}
	}
	return ""
}

func (libstdTransitionMutator) Mutate(ctx android.BottomUpMutatorContext, variation string) {
	switch variation {
	case "rlib-std":
		rlib := ctx.Module().(*Module)
		rlib.compiler.(libraryInterface).setRlibStd()
		rlib.Properties.RustSubName += RlibStdlibSuffix
	case "rlib-core":
		rlib := ctx.Module().(*Module)
		rlib.compiler.(libraryInterface).setRlibStd()
		rlib.compiler.(libraryInterface).setNoStd()
		rlib.Properties.RustSubName += CoreStdlibSuffix
	case "dylib-std":
		dylib := ctx.Module().(*Module)
		dylib.compiler.(libraryInterface).setDylibStd()
		if dylib.ModuleBase.ImageVariation().Variation == android.VendorRamdiskVariation {
			// TODO(b/165791368)
			// Disable rlibs that link against dylib-std on vendor ramdisk variations until those dylib
			// variants are properly supported.
			dylib.Disable()
		}
	}
}

func (library *libraryDecorator) variantProperties() *VariantLibraryProperties {
	if library.noStd() && library.Properties.No_std != nil {
		return library.Properties.No_std
	} else if library.rlib() {
		return &library.Properties.Rlib
	} else if library.dylib() {
		return &library.Properties.Dylib
	} else if library.shared() {
		return &library.Properties.Shared
	} else if library.static() {
		return &library.Properties.Static
	} else {
		return nil
	}
}

func (library *libraryDecorator) begin(ctx BaseModuleContext) {
	library.baseCompiler.begin(ctx)

	if overrides := library.variantProperties(); overrides != nil {
		if err := proptools.ExtendMatchingProperties(ctx.Module().GetProperties(), overrides, nil, proptools.OrderReplace); err != nil {
			panic(fmt.Errorf("unable to apply overrides: %v", err))
		}
	}
	stdlibs := library.baseCompiler.Properties.Stdlibs.Get(ctx)
	if library.stdLinkage(ctx.Device()) == RlibCore && !stdlibs.IsPresent() {
		library.baseCompiler.Properties.Stdlibs = proptools.NewSimpleConfigurable(config.Corelibs)
	}
}
