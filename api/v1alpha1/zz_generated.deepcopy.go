package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func (in *ModelArtifact) DeepCopyInto(out *ModelArtifact) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	out.Spec = in.Spec
	in.Status.DeepCopyInto(&out.Status)
}

func (in *ModelArtifact) DeepCopy() *ModelArtifact {
	if in == nil {
		return nil
	}
	out := new(ModelArtifact)
	in.DeepCopyInto(out)
	return out
}

func (in *ModelArtifact) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

func (in *ModelArtifactStatus) DeepCopyInto(out *ModelArtifactStatus) {
	*out = *in
	if in.LastVerifiedAt != nil {
		in, out := &in.LastVerifiedAt, &out.LastVerifiedAt
		*out = (*in).DeepCopy()
	}
	if in.ValidUntil != nil {
		in, out := &in.ValidUntil, &out.ValidUntil
		*out = (*in).DeepCopy()
	}
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]metav1.Condition, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

func (in *ModelArtifactList) DeepCopyInto(out *ModelArtifactList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]ModelArtifact, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

func (in *ModelArtifactList) DeepCopy() *ModelArtifactList {
	if in == nil {
		return nil
	}
	out := new(ModelArtifactList)
	in.DeepCopyInto(out)
	return out
}

func (in *ModelArtifactList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

func (in *TrustedPrincipalSpec) DeepCopyInto(out *TrustedPrincipalSpec) {
	*out = *in
	if in.SecretRef != nil {
		in, out := &in.SecretRef, &out.SecretRef
		*out = new(SecretKeySelector)
		**out = **in
	}
}

func (in *ClusterModelTrustPolicySpec) DeepCopyInto(out *ClusterModelTrustPolicySpec) {
	*out = *in
	if in.AllowedRegistries != nil {
		out.AllowedRegistries = append([]string(nil), in.AllowedRegistries...)
	}
	if in.TrustDomains.ModelSigners != nil {
		out.TrustDomains.ModelSigners = make([]TrustedPrincipalSpec, len(in.TrustDomains.ModelSigners))
		for i := range in.TrustDomains.ModelSigners {
			in.TrustDomains.ModelSigners[i].DeepCopyInto(&out.TrustDomains.ModelSigners[i])
		}
	}
	if in.TrustDomains.Evaluators != nil {
		out.TrustDomains.Evaluators = make([]TrustedPrincipalSpec, len(in.TrustDomains.Evaluators))
		for i := range in.TrustDomains.Evaluators {
			in.TrustDomains.Evaluators[i].DeepCopyInto(&out.TrustDomains.Evaluators[i])
		}
	}
	if in.TrustDomains.ReleaseAuthorities != nil {
		out.TrustDomains.ReleaseAuthorities = make([]TrustedPrincipalSpec, len(in.TrustDomains.ReleaseAuthorities))
		for i := range in.TrustDomains.ReleaseAuthorities {
			in.TrustDomains.ReleaseAuthorities[i].DeepCopyInto(&out.TrustDomains.ReleaseAuthorities[i])
		}
	}
	if in.Exemptions.ExemptServiceAccounts != nil {
		out.Exemptions.ExemptServiceAccounts = append([]string(nil), in.Exemptions.ExemptServiceAccounts...)
	}
	if in.Exemptions.ExemptPodLabels != nil {
		out.Exemptions.ExemptPodLabels = make(map[string]string, len(in.Exemptions.ExemptPodLabels))
		for k, v := range in.Exemptions.ExemptPodLabels {
			out.Exemptions.ExemptPodLabels[k] = v
		}
	}
}

func (in *ClusterModelTrustPolicyStatus) DeepCopyInto(out *ClusterModelTrustPolicyStatus) {
	*out = *in
	if in.ResolvedModelSignerFingerprints != nil {
		out.ResolvedModelSignerFingerprints = append([]string(nil), in.ResolvedModelSignerFingerprints...)
	}
	if in.ResolvedEvaluatorFingerprints != nil {
		out.ResolvedEvaluatorFingerprints = append([]string(nil), in.ResolvedEvaluatorFingerprints...)
	}
	if in.ResolvedReleaseAuthorityFingerprints != nil {
		out.ResolvedReleaseAuthorityFingerprints = append([]string(nil), in.ResolvedReleaseAuthorityFingerprints...)
	}
	if in.LastReconciledAt != nil {
		in, out := &in.LastReconciledAt, &out.LastReconciledAt
		*out = (*in).DeepCopy()
	}
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]metav1.Condition, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

func (in *ClusterModelTrustPolicy) DeepCopyInto(out *ClusterModelTrustPolicy) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

func (in *ClusterModelTrustPolicy) DeepCopy() *ClusterModelTrustPolicy {
	if in == nil {
		return nil
	}
	out := new(ClusterModelTrustPolicy)
	in.DeepCopyInto(out)
	return out
}

func (in *ClusterModelTrustPolicy) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

func (in *ClusterModelTrustPolicyList) DeepCopyInto(out *ClusterModelTrustPolicyList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]ClusterModelTrustPolicy, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

func (in *ClusterModelTrustPolicyList) DeepCopy() *ClusterModelTrustPolicyList {
	if in == nil {
		return nil
	}
	out := new(ClusterModelTrustPolicyList)
	in.DeepCopyInto(out)
	return out
}

func (in *ClusterModelTrustPolicyList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}
