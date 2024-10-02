// Copyright 2023 Google Inc. All rights reserved.
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

package build_flags

import (
	"path/filepath"

	"android/soong/android"

	"github.com/google/blueprint"
)

type ReleaseConfigContributionsProviderData struct {
	ContributionDir android.SourcePath
}

var ReleaseConfigContributionsProviderKey = blueprint.NewProvider[ReleaseConfigContributionsProviderData]()

// Soong uses `release_config_contributions` modules to produce the
// `build_flags/all_release_config_contributions.*` artifacts, listing *all* of
// the directories in the source tree that contribute to each release config,
// whether or not they are actually used for the lunch product.
//
// This artifact helps flagging automation determine in which directory a flag
// should be placed by default.
type ReleaseConfigContributionsModule struct {
	android.ModuleBase
	android.DefaultableModuleBase

	// Properties for "release_config_contributions"
	properties struct {
		// The `release_configs/*.textproto` files provided by this
		// directory, relative to this Android.bp file
		Srcs []string `android:"path"`
	}
}

func ReleaseConfigContributionsFactory() android.Module {
	module := &ReleaseConfigContributionsModule{}

	android.InitAndroidModule(module)
	android.InitDefaultableModule(module)
	module.AddProperties(&module.properties)

	return module
}

func (module *ReleaseConfigContributionsModule) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	srcs := android.PathsForModuleSrc(ctx, module.properties.Srcs)
	if len(srcs) == 0 {
		return
	}
	contributionDir := filepath.Dir(filepath.Dir(srcs[0].String()))
	for _, file := range srcs {
		if filepath.Dir(filepath.Dir(file.String())) != contributionDir {
			ctx.ModuleErrorf("Cannot include %s with %s contributions", file, contributionDir)
		}
		if filepath.Base(filepath.Dir(file.String())) != "release_configs" || file.Ext() != ".textproto" {
			ctx.ModuleErrorf("Invalid contribution file %s", file)
		}
	}
	android.SetProvider(ctx, ReleaseConfigContributionsProviderKey, ReleaseConfigContributionsProviderData{
		ContributionDir: android.PathForSource(ctx, contributionDir),
	})

}
