package image

import (
	"github.com/google/go-containerregistry/pkg/name"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"

	"github.com/pivotal/kpack/pkg/apis/build/v1alpha1"
)

func buildNeeded(im *v1alpha1.Image, lastBuild *v1alpha1.Build, sourceResolver *v1alpha1.SourceResolver, builder v1alpha1.BuilderResource) (map[string]string, corev1.ConditionStatus) {
	if !sourceResolver.Ready() || !builder.Ready() {
		return nil, corev1.ConditionUnknown
	}

	if lastBuild == nil || im.Spec.Tag != lastBuild.Tag() {
		return map[string]string{
			v1alpha1.BuildReasonConfig: "",
		}, corev1.ConditionTrue
	}

	var reasons map[string]interface{}

	if sourceResolver.ConfigChanged(lastBuild) ||
		!equality.Semantic.DeepEqual(im.Env(), lastBuild.Spec.Env) ||
		!equality.Semantic.DeepEqual(im.Resources(), lastBuild.Spec.Resources) ||
		!equality.Semantic.DeepEqual(im.Bindings(), lastBuild.Spec.Bindings) {
		reasons[v1alpha1.BuildReasonConfig] = ""
		//reasons = append(reasons, v1alpha1.BuildReasonConfig)
	}

	revisionChange := sourceResolver.RevisionChange(lastBuild)
	if revisionChange.HasChanged() {
		reasons[v1alpha1.BuildReasonCommit] = revisionChange
	}

	if lastBuild.IsSuccess() {
		buildpacksChange := builtWithBuildpacks(lastBuild, builder.BuildpackMetadata())
		if buildpacksChange.hasChanged() {
			reasons[v1alpha1.BuildReasonBuildpack] = buildpacksChange.Buildpacks
		}

		stackChange := builtWithStack(lastBuild, builder.RunImage())
		if stackChange.hasChanged() {
			reasons[v1alpha1.BuildReasonStack] = stackChange
		}
	}

	if additionalBuildNeeded(lastBuild) {
		reasons[v1alpha1.BuildReasonBuildpack] = ""
		//reasons = append(reasons, v1alpha1.BuildReasonTrigger)
	}

	if len(reasons) == 0 {
		return nil, corev1.ConditionFalse
	}

	return reasons, corev1.ConditionTrue
}

type BuildpackChange struct {
	Buildpacks []BuildpackInfo
}

type BuildpackInfo struct {
	id      string
	version string
}

func (bc *BuildpackChange) hasChanged() bool {
	return len(bc.Buildpacks) > 0
}

func builtWithBuildpacks(build *v1alpha1.Build, buildpacks v1alpha1.BuildpackMetadataList) BuildpackChange {
	buildpackInfos := []BuildpackInfo{}
	for _, bp := range build.Status.BuildMetadata {
		if !buildpacks.Include(bp) {
			buildpackInfos = append(buildpackInfos, BuildpackInfo{bp.Id, bp.Version})
		}
	}

	return BuildpackChange{buildpackInfos}
}

type StackChange struct {
	LastBuildRunImage string
	BuilderRunImage   string
}

func (sc *StackChange) hasChanged() bool {
	return sc.LastBuildRunImage != sc.BuilderRunImage
}

func builtWithStack(build *v1alpha1.Build, runImage string) StackChange {
	var stackChange StackChange
	if build.Status.Stack.RunImage == "" {
		return stackChange
	}

	lastBuildRunImageRef, err := name.ParseReference(build.Status.Stack.RunImage)
	if err != nil {
		return stackChange
	}

	builderRunImageRef, err := name.ParseReference(runImage)
	if err != nil {
		return stackChange
	}

	return StackChange{
		LastBuildRunImage: lastBuildRunImageRef.Identifier(),
		BuilderRunImage:   builderRunImageRef.Identifier(),
	}
}

func additionalBuildNeeded(build *v1alpha1.Build) bool {
	_, ok := build.Annotations[v1alpha1.BuildNeededAnnotation]
	return ok
}
