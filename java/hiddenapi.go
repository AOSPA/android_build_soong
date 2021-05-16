// Copyright 2019 Google Inc. All rights reserved.
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

package java

import (
	"github.com/google/blueprint"

	"android/soong/android"
)

var hiddenAPIGenerateCSVRule = pctx.AndroidStaticRule("hiddenAPIGenerateCSV", blueprint.RuleParams{
	Command:     "${config.Class2NonSdkList} --stub-api-flags ${stubAPIFlags} $in $outFlag $out",
	CommandDeps: []string{"${config.Class2NonSdkList}"},
}, "outFlag", "stubAPIFlags")

type hiddenAPI struct {
	// The name of the module as it would be used in the boot jars configuration, e.g. without any
	// prebuilt_ prefix (if it is a prebuilt) and without any ".impl" suffix if it is a
	// java_sdk_library implementation library.
	configurationName string

	// True if the module containing this structure contributes to the hiddenapi information or has
	// that information encoded within it.
	active bool

	// Identifies the active module variant which will be used as the source of hiddenapi information.
	//
	// A class may be compiled into a number of different module variants each of which will need the
	// hiddenapi information encoded into it and so will be marked as active. However, only one of
	// them must be used as a source of information by hiddenapi otherwise it will end up with
	// duplicate entries. That module will have primary=true.
	//
	// Note, that modules <x>-hiddenapi that provide additional annotation information for module <x>
	// that is on the bootclasspath are marked as primary=true as they are the primary source of that
	// annotation information.
	primary bool

	// The path to the dex jar that is in the boot class path. If this is nil then the associated
	// module is not a boot jar, but could be one of the <x>-hiddenapi modules that provide additional
	// annotations for the <x> boot dex jar but which do not actually provide a boot dex jar
	// themselves.
	//
	// This must be the path to the unencoded dex jar as the encoded dex jar indirectly depends on
	// this file so using the encoded dex jar here would result in a cycle in the ninja rules.
	bootDexJarPath android.Path

	// The paths to the classes jars that contain classes and class members annotated with
	// the UnsupportedAppUsage annotation that need to be extracted as part of the hidden API
	// processing.
	classesJarPaths android.Paths
}

func (h *hiddenAPI) bootDexJar() android.Path {
	return h.bootDexJarPath
}

func (h *hiddenAPI) classesJars() android.Paths {
	return h.classesJarPaths
}

// hiddenAPIModule is the interface a module that embeds the hiddenAPI structure must implement.
type hiddenAPIModule interface {
	android.Module
	hiddenAPIIntf
}

type hiddenAPIIntf interface {
	bootDexJar() android.Path
	classesJars() android.Paths
}

var _ hiddenAPIIntf = (*hiddenAPI)(nil)

// Initialize the hiddenapi structure
func (h *hiddenAPI) initHiddenAPI(ctx android.BaseModuleContext, configurationName string) {
	// If hiddenapi processing is disabled treat this as inactive.
	if ctx.Config().IsEnvTrue("UNSAFE_DISABLE_HIDDENAPI_FLAGS") {
		return
	}

	h.configurationName = configurationName

	// If the frameworks/base directories does not exist and no prebuilt hidden API flag files have
	// been configured then it is not possible to do hidden API encoding.
	if !ctx.Config().FrameworksBaseDirExists(ctx) && ctx.Config().PrebuiltHiddenApiDir(ctx) == "" {
		return
	}

	// It is important that hiddenapi information is only gathered for/from modules that are actually
	// on the boot jars list because the runtime only enforces access to the hidden API for the
	// bootclassloader. If information is gathered for modules not on the list then that will cause
	// failures in the CtsHiddenApiBlocklist... tests.
	module := ctx.Module()
	h.active = isModuleInBootClassPath(ctx, module)
	if !h.active {
		// The rest of the properties will be ignored if active is false.
		return
	}

	// Determine whether this module is the primary module or not.
	primary := true

	// A prebuilt module is only primary if it is preferred and conversely a source module is only
	// primary if it has not been replaced by a prebuilt module.
	if pi, ok := module.(android.PrebuiltInterface); ok {
		if p := pi.Prebuilt(); p != nil {
			primary = p.UsePrebuilt()
		}
	} else {
		// The only module that will pass a different configurationName to its module name to this
		// method is the implementation library of a java_sdk_library. It has a configuration name of
		// <x> the same as its parent java_sdk_library but a module name of <x>.impl. It is not the
		// primary module, the java_sdk_library with the name of <x> is.
		primary = configurationName == ctx.ModuleName()

		// A source module that has been replaced by a prebuilt can never be the primary module.
		if module.IsReplacedByPrebuilt() {
			if ctx.HasProvider(android.ApexInfoProvider) {
				// The source module is in an APEX but the prebuilt module on which it depends is not in an
				// APEX and so is not the one that will actually be used for hidden API processing. That
				// means it is not possible to check to see if it is a suitable replacement so just assume
				// that it is.
				primary = false
			} else {
				ctx.VisitDirectDepsWithTag(android.PrebuiltDepTag, func(prebuilt android.Module) {
					if h, ok := prebuilt.(hiddenAPIIntf); ok && h.bootDexJar() != nil {
						primary = false
					} else {
						ctx.ModuleErrorf(
							"hiddenapi has determined that the source module %q should be ignored as it has been"+
								" replaced by the prebuilt module %q but unfortunately it does not provide a"+
								" suitable boot dex jar", ctx.ModuleName(), ctx.OtherModuleName(prebuilt))
					}
				})
			}
		}
	}
	h.primary = primary
}

func isModuleInBootClassPath(ctx android.BaseModuleContext, module android.Module) bool {
	// Get the configured non-updatable and updatable boot jars.
	nonUpdatableBootJars := ctx.Config().NonUpdatableBootJars()
	updatableBootJars := ctx.Config().UpdatableBootJars()
	active := isModuleInConfiguredList(ctx, module, nonUpdatableBootJars) ||
		isModuleInConfiguredList(ctx, module, updatableBootJars)
	return active
}

// hiddenAPIEncodeDex is called by any module that needs to encode dex files.
//
// It ignores any module that has not had initHiddenApi() called on it and which is not in the boot
// jar list. In that case it simply returns the supplied dex jar path.
//
// Otherwise, it creates a copy of the supplied dex file into which it has encoded the hiddenapi
// flags and returns this instead of the supplied dex jar.
func (h *hiddenAPI) hiddenAPIEncodeDex(ctx android.ModuleContext, dexJar android.OutputPath, uncompressDex bool) android.OutputPath {

	if !h.active {
		return dexJar
	}

	hiddenAPIJar := android.PathForModuleOut(ctx, "hiddenapi", h.configurationName+".jar").OutputPath

	// Create a copy of the dex jar which has been encoded with hiddenapi flags.
	hiddenAPIEncodeDex(ctx, hiddenAPIJar, dexJar, uncompressDex)

	// Use the encoded dex jar from here onwards.
	dexJar = hiddenAPIJar

	return dexJar
}

// hiddenAPIUpdatePaths generates ninja rules to extract the information from the classes
// jar, and outputs it to the appropriate module specific CSV file.
//
// It also makes the dex jar available for use when generating the
// hiddenAPISingletonPathsStruct.stubFlags.
func (h *hiddenAPI) hiddenAPIUpdatePaths(ctx android.ModuleContext, dexJar, classesJar android.Path) {

	// Save the classes jars even if this is not active as they may be used by modular hidden API
	// processing.
	classesJars := android.Paths{classesJar}
	ctx.VisitDirectDepsWithTag(hiddenApiAnnotationsTag, func(dep android.Module) {
		javaInfo := ctx.OtherModuleProvider(dep, JavaInfoProvider).(JavaInfo)
		classesJars = append(classesJars, javaInfo.ImplementationJars...)
	})
	h.classesJarPaths = classesJars

	// Save the unencoded dex jar so it can be used when generating the
	// hiddenAPISingletonPathsStruct.stubFlags file.
	h.bootDexJarPath = dexJar
}

// buildRuleToGenerateAnnotationFlags builds a ninja rule to generate the annotation-flags.csv file
// from the classes jars and stub-flags.csv files.
//
// The annotation-flags.csv file contains mappings from Java signature to various flags derived from
// annotations in the source, e.g. whether it is public or the sdk version above which it can no
// longer be used.
//
// It is created by the Class2NonSdkList tool which processes the .class files in the class
// implementation jar looking for UnsupportedAppUsage and CovariantReturnType annotations. The
// tool also consumes the hiddenAPISingletonPathsStruct.stubFlags file in order to perform
// consistency checks on the information in the annotations and to filter out bridge methods
// that are already part of the public API.
func buildRuleToGenerateAnnotationFlags(ctx android.ModuleContext, desc string, classesJars android.Paths, stubFlagsCSV android.Path, outputPath android.WritablePath) {
	ctx.Build(pctx, android.BuildParams{
		Rule:        hiddenAPIGenerateCSVRule,
		Description: desc,
		Inputs:      classesJars,
		Output:      outputPath,
		Implicit:    stubFlagsCSV,
		Args: map[string]string{
			"outFlag":      "--write-flags-csv",
			"stubAPIFlags": stubFlagsCSV.String(),
		},
	})
}

// buildRuleToGenerateMetadata builds a ninja rule to generate the metadata.csv file from
// the classes jars and stub-flags.csv files.
//
// The metadata.csv file contains mappings from Java signature to the value of properties specified
// on UnsupportedAppUsage annotations in the source.
//
// Like the annotation-flags.csv file this is also created by the Class2NonSdkList in the same way.
// Although the two files could potentially be created in a single invocation of the
// Class2NonSdkList at the moment they are created using their own invocation, with the behavior
// being determined by the property that is used.
func buildRuleToGenerateMetadata(ctx android.ModuleContext, desc string, classesJars android.Paths, stubFlagsCSV android.Path, metadataCSV android.WritablePath) {
	ctx.Build(pctx, android.BuildParams{
		Rule:        hiddenAPIGenerateCSVRule,
		Description: desc,
		Inputs:      classesJars,
		Output:      metadataCSV,
		Implicit:    stubFlagsCSV,
		Args: map[string]string{
			"outFlag":      "--write-metadata-csv",
			"stubAPIFlags": stubFlagsCSV.String(),
		},
	})
}

// buildRuleToGenerateIndex builds a ninja rule to generate the index.csv file from the classes
// jars.
//
// The index.csv file contains mappings from Java signature to source location information.
//
// It is created by the merge_csv tool which processes the class implementation jar, extracting
// all the files ending in .uau (which are CSV files) and merges them together. The .uau files are
// created by the unsupported app usage annotation processor during compilation of the class
// implementation jar.
func buildRuleToGenerateIndex(ctx android.ModuleContext, desc string, classesJars android.Paths, indexCSV android.WritablePath) {
	rule := android.NewRuleBuilder(pctx, ctx)
	rule.Command().
		BuiltTool("merge_csv").
		Flag("--zip_input").
		Flag("--key_field signature").
		FlagWithArg("--header=", "signature,file,startline,startcol,endline,endcol,properties").
		FlagWithOutput("--output=", indexCSV).
		Inputs(classesJars)
	rule.Build(desc, desc)
}

var hiddenAPIEncodeDexRule = pctx.AndroidStaticRule("hiddenAPIEncodeDex", blueprint.RuleParams{
	Command: `rm -rf $tmpDir && mkdir -p $tmpDir && mkdir $tmpDir/dex-input && mkdir $tmpDir/dex-output &&
		unzip -qoDD $in 'classes*.dex' -d $tmpDir/dex-input &&
		for INPUT_DEX in $$(find $tmpDir/dex-input -maxdepth 1 -name 'classes*.dex' | sort); do
		  echo "--input-dex=$${INPUT_DEX}";
		  echo "--output-dex=$tmpDir/dex-output/$$(basename $${INPUT_DEX})";
		done | xargs ${config.HiddenAPI} encode --api-flags=$flagsCsv $hiddenapiFlags &&
		${config.SoongZipCmd} $soongZipFlags -o $tmpDir/dex.jar -C $tmpDir/dex-output -f "$tmpDir/dex-output/classes*.dex" &&
		${config.MergeZipsCmd} -D -zipToNotStrip $tmpDir/dex.jar -stripFile "classes*.dex" -stripFile "**/*.uau" $out $tmpDir/dex.jar $in`,
	CommandDeps: []string{
		"${config.HiddenAPI}",
		"${config.SoongZipCmd}",
		"${config.MergeZipsCmd}",
	},
}, "flagsCsv", "hiddenapiFlags", "tmpDir", "soongZipFlags")

func hiddenAPIEncodeDex(ctx android.ModuleContext, output android.WritablePath, dexInput android.Path,
	uncompressDex bool) {

	flagsCSV := hiddenAPISingletonPaths(ctx).flags

	// The encode dex rule requires unzipping and rezipping the classes.dex files, ensure that if it was uncompressed
	// in the input it stays uncompressed in the output.
	soongZipFlags := ""
	hiddenapiFlags := ""
	tmpOutput := output
	tmpDir := android.PathForModuleOut(ctx, "hiddenapi", "dex")
	if uncompressDex {
		soongZipFlags = "-L 0"
		tmpOutput = android.PathForModuleOut(ctx, "hiddenapi", "unaligned", "unaligned.jar")
		tmpDir = android.PathForModuleOut(ctx, "hiddenapi", "unaligned")
	}

	enforceHiddenApiFlagsToAllMembers := true

	// b/149353192: when a module is instrumented, jacoco adds synthetic members
	// $jacocoData and $jacocoInit. Since they don't exist when building the hidden API flags,
	// don't complain when we don't find hidden API flags for the synthetic members.
	if j, ok := ctx.Module().(interface {
		shouldInstrument(android.BaseModuleContext) bool
	}); ok && j.shouldInstrument(ctx) {
		enforceHiddenApiFlagsToAllMembers = false
	}

	if !enforceHiddenApiFlagsToAllMembers {
		hiddenapiFlags = "--no-force-assign-all"
	}

	ctx.Build(pctx, android.BuildParams{
		Rule:        hiddenAPIEncodeDexRule,
		Description: "hiddenapi encode dex",
		Input:       dexInput,
		Output:      tmpOutput,
		Implicit:    flagsCSV,
		Args: map[string]string{
			"flagsCsv":       flagsCSV.String(),
			"tmpDir":         tmpDir.String(),
			"soongZipFlags":  soongZipFlags,
			"hiddenapiFlags": hiddenapiFlags,
		},
	})

	if uncompressDex {
		TransformZipAlign(ctx, output, tmpOutput)
	}
}

type hiddenApiAnnotationsDependencyTag struct {
	blueprint.BaseDependencyTag
}

// Tag used to mark dependencies on java_library instances that contains Java source files whose
// sole purpose is to provide additional hiddenapi annotations.
var hiddenApiAnnotationsTag hiddenApiAnnotationsDependencyTag

// Mark this tag so dependencies that use it are excluded from APEX contents.
func (t hiddenApiAnnotationsDependencyTag) ExcludeFromApexContents() {}

var _ android.ExcludeFromApexContentsTag = hiddenApiAnnotationsTag