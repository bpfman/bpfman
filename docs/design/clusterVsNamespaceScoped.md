# bpfman CRDs - Cluster vs Namespace Scoped

## Status

This design was implemented with
[bpfman-operator pull request #344](https://github.com/bpfman/bpfman-operator/pull/344).
The feature was first releases in the bpfman-operator v0.5.5 release.


## Introduction

For security reasons, cluster admins may want to limit certain applications to only loading eBPF programs
within a given namespace.
Currently, all bpfman Custom Resource Definitions (CRDs) are Cluster scoped.
To provide cluster admins with tighter controls on eBPF program loading, some of the bpfman CRDs also need
to be Namespace scoped.

Not all eBPF programs make sense to be namespaced scoped.
Some eBPF programs like kprobe cannot be constrained to a namespace.
The following programs will have a namespaced scoped variant:

* Uprobe
* TC
* TCX
* XDP

There will also be a namespace scoped BpfApplication variant that is limited to namespaced scoped
eBPF programs listed above.

## Current Implementation

Currently, the reconciler code is broken into two layers (for both the bpfman-operator and the bpfman-agent).
There is the \*Program layer, where there is a reconcile for each program type (Fentry, Fexit, Kprobe, etc).
At this layer, the program specific code handles creating the program specific structure.
The \*Program layer then calls the Common layer to handle processing that is common across all programs.

There are some structures, then an interface that defines the set of methods the structure needs to support.

### struct

There are a set of structures (one for the BPF Program CRD and then one for each \*Program CRD) that define the
contents of the CRDs (bpfman-operator/apis/v1alpha).
Each object (BPF Program CRD and \*Program CRD) also has a List object.

```go
type BpfProgram struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec BpfProgramSpec `json:"spec"`
	// +optional
	Status BpfProgramStatus `json:"status,omitempty"`
}
type BpfProgramList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BpfProgram `json:"items"`
}

type FentryProgram struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec FentryProgramSpec `json:"spec"`
	// +optional
	Status FentryProgramStatus `json:"status,omitempty"`
}
type FentryProgramList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []FentryProgram `json:"items"`
}

:
```

There is a reconciler for each \*Program.
For the implementation, there is a common set of data used by each \*Program reconciler which is
contained in the base struct `ReconcilerCommon`.
Then there is a \*Program struct, which includes each \*Program’s Program struct and the base
struct `ReconcilerCommon`.
Below are the bpfman-agent structures, but the bpfman-operator follows the same pattern.

```go
type ReconcilerCommon struct {
	client.Client
	Scheme       *runtime.Scheme
	GrpcConn     *grpc.ClientConn
	BpfmanClient gobpfman.BpfmanClient
	Logger       logr.Logger
	NodeName     string
	progId       *uint32
	finalizer    string
	recType      string
	appOwner     metav1.Object // Set if the owner is an application
}

type FentryProgramReconciler struct {
	ReconcilerCommon
	currentFentryProgram *bpfmaniov1alpha1.FentryProgram
	ourNode              *v1.Node
}

type FexitProgramReconciler struct {
	ReconcilerCommon
	currentFexitProgram *bpfmaniov1alpha1.FexitProgram
	ourNode             *v1.Node
}

:
```

### interface

The `bpfmanReconciler` interface defines the set of methods the \*Program structs must implement to use the
common reconciler code. 
Below are the bpfman-agent structures, but the bpfman-operator uses a `ProgramReconciler`, which follows
the same pattern.

```go
type bpfmanReconciler interface {
	SetupWithManager(mgr ctrl.Manager) error
	Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error)
	getFinalizer() string
	getOwner() metav1.Object
	getRecType() string
	getProgType() internal.ProgramType
	getName() string
	getExpectedBpfPrograms(ctx context.Context)
    (*bpfmaniov1alpha1.BpfProgramList, error)
	getLoadRequest(bpfProgram *bpfmaniov1alpha1.BpfProgram,
    mapOwnerId *uint32) (*gobpfman.LoadRequest, error)
	getNode() *v1.Node
	getBpfProgramCommon() *bpfmaniov1alpha1.BpfProgramCommon
	setCurrentProgram(program client.Object) error
	getNodeSelector() *metav1.LabelSelector
	getBpfGlobalData() map[string][]byte
	getAppProgramId() string
}
```

There are also some common reconciler functions that perform common code.

```go
func (r *ReconcilerCommon) reconcileCommon(ctx context.Context,
  rec bpfmanReconciler,
	programs []client.Object) (bool, ctrl.Result, error) {
:
}

func (r *ReconcilerCommon) reconcileBpfProgram(ctx context.Context,
	rec bpfmanReconciler,
	loadedBpfPrograms map[string]*gobpfman.ListResponse_ListResult,
	bpfProgram *bpfmaniov1alpha1.BpfProgram,
	isNodeSelected bool,
	isBeingDeleted bool,
	mapOwnerStatus *MapOwnerParamStatus)
(bpfmaniov1alpha1.BpfProgramConditionType, error) {
:
}

func (r *ReconcilerCommon) reconcileBpfProgramSuccessCondition(
	isLoaded bool,
	shouldBeLoaded bool,
	isNodeSelected bool,
	isBeingDeleted bool,
	noContainersOnNode bool,
  mapOwnerStatus *MapOwnerParamStatus) bpfmaniov1alpha1.BpfProgramConditionType {
:
}
```

So looks something like this:

```
                     --- FentryProgramReconciler
                     |     func (r *FentryProgramReconciler) getFinalizer() string {}
                     |
bpfmanReconciler   ----- FexitProgramReconciler
  ReconcilerCommon   |     func (r *FexitProgramReconciler) getFinalizer() string {}
                     |
                     --- …
```

## Adding Namespaced Scoped CRDs

While the contents are mostly the same for the namespace and cluster-scoped CRD in most cases, Kubernetes
requires different  CRD for each type.

### struct

The set of CRD structures will need to be duplicated for each Namespaced scoped CRD (bpfman-operator/apis/v1alpha).
Note, data is the similar, just a new object.
The primary change is the existing `ContainerSelector` struct will be replaced with a `ContainerNsSelector`.
For Namespaced scoped CRDs, the `namespace` in the `ContainerSelector` is removed.
The `Namespace` field for the object is embedded the `metav1.ObjectMeta` structure.
Not all Program Types will have a Namespaced version, only those that can be contained by a namespace:

* TC
* TCX
* Uprobe
* XDP

The Application Program will also have a namespaced version, but it will only allow the Program Types
that are namespaced.

```go
type BpfProgram struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec BpfProgramSpec `json:"spec"`
	// +optional
	Status BpfProgramStatus `json:"status,omitempty"`
}
type BpfProgramList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BpfProgram `json:"items"`
}

type BpfNsProgram struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec BpfProgramSpec `json:"spec"`
	// +optional
	Status BpfProgramStatus `json:"status,omitempty"`
}
type BpfNsProgramList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BpfProgram `json:"items"`
}

type TcProgram struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec TcProgramSpec `json:"spec"`
	// +optional
	Status TcProgramStatus `json:"status,omitempty"`
}
type TcProgramList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TcProgram `json:"items"`
}

type TcNsProgram struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec TcNsProgramSpec `json:"spec"`
	// +optional
	Status TcProgramStatus `json:"status,omitempty"`
}
type TcNsProgramList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TcNsProgram `json:"items"`
}

:
```

### interface

The problem is that the `bpfmanReconciler` interface and common functions use the types `bpfmanReconciler`,
`BpfProgram` and `BpfProgramList`, which will need to be cluster or namespaced objects.

To allow the common code to act on both Cluster or Namespaced objects, two new interfaces will be introduced.
First is `BpfProg`.
Both `BpfProgram` and `BpfNsProgram` need to implement these functions.

```go
type BpfProg interface {
	GetName() string
	GetUID() types.UID
	GetAnnotations() map[string]string
	GetLabels() map[string]string
	GetStatus() *bpfmaniov1alpha1.BpfProgramStatus
	GetClientObject() client.Object
}
```

The second interface is `BpfProgList`.
Both `BpfProgramList` and `BpfNsProgramList` will need to implement these functions.
Because the list objects have lists of the `BpfProgram`or `BpfNsProgram`, the base interface is a generic,
where type T can be a either `BpfProgram` or `BpfNsProgram`.

```go
type BpfProgList[T any] interface {
	GetItems() []T
}
```

The reconciler base struct `ReconcilerCommon` then becomes a generic as well, and all references to the types
`bpfmanReconciler`, `BpfProgram` and `BpfProgramList` become the types `bpfmanReconciler[T,TL]`, `T` and `TL`.
Below are the bpfman-agent structures, but the bpfman-operator follows the same pattern.

```go
type ReconcilerCommon[T BpfProg, TL BpfProgList[T]] struct {
	: // Data is the same
}

func (r *ReconcilerCommon) reconcileCommon(ctx context.Context,
rec bpfmanReconciler[T, TL],
	programs []client.Object) (bool, ctrl.Result, error) {
:
}

func (r *ReconcilerCommon) reconcileBpfProgram(ctx context.Context,
	rec bpfmanReconciler[T, TL],
	loadedBpfPrograms map[string]*gobpfman.ListResponse_ListResult,
	bpfProgram *T,
	isNodeSelected bool,
	isBeingDeleted bool,
	mapOwnerStatus *MapOwnerParamStatus)
(bpfmaniov1alpha1.BpfProgramConditionType, error) {
:
}

func (r *ReconcilerCommon) reconcileBpfProgramSuccessCondition(
	isLoaded bool,
	shouldBeLoaded bool,
	isNodeSelected bool,
	isBeingDeleted bool,
	noContainersOnNode bool,
  mapOwnerStatus *MapOwnerParamStatus) bpfmaniov1alpha1.BpfProgramConditionType {
:
}
```

Same for the `bpfmanReconciler` interface. 

```go
type bpfmanReconciler[T BpfProg, TL BpfProgList[T]] interface {
	SetupWithManager(mgr ctrl.Manager) error
	Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error)
	getFinalizer() string
	getOwner() metav1.Object
	getRecType() string
	getProgType() internal.ProgramType
	getName() string
	getExpectedBpfPrograms(ctx context.Context)(*TL, error)
	getLoadRequest(bpfProgram *T,
    mapOwnerId *uint32) (*gobpfman.LoadRequest, error)
	getNode() *v1.Node
	getBpfProgramCommon() *bpfmaniov1alpha1.BpfProgramCommon
	setCurrentProgram(program client.Object) error
	getNodeSelector() *metav1.LabelSelector
	getBpfGlobalData() map[string][]byte
	getAppProgramId() string
}
```

Issues arose when `ReconcilerCommon` functions needed to modify the `BpfProgram` or `BpfNsProgram` data.
For the modifications to be applied, the types need to be pointers `bpfmanReconciler[*T, *TL]`, `*T` and `*TL`.
However, the compiler would not allow this:

    cannot use type BpfProgList[*T] outside a type constraint: interface contains type constraints

To work around this, a new layer was added.
A struct for cluster scoped code and a one for namespaced code.
So looks something like this:

```
                     +--- ClusterProgramReconciler
                     |     |
                     |     +--- FentryProgramReconciler
                     |     |     func (r *FentryProgramReconciler) getFinalizer() string {}
                     |     |     :
                     |     |
                     |     +--- FexitProgramReconciler
                     |     |     func (r *FexitProgramReconciler) getFinalizer() string {}
                     |     |     :
                     |     :
bpfmanReconciler   --+
  ReconcilerCommon   |
                     +--- NamespaceProgramReconciler
                           |
                           +--- FentryNsProgramReconciler
                           |     func (r *FentryProgramReconciler) getFinalizer() string {}
                           |     :
                           |
                           +--- FexitNsProgramReconciler
                           |     func (r *FexitProgramReconciler) getFinalizer() string {}
                           |     :
                           :
```

```go
type ClusterProgramReconciler struct {
	ReconcilerCommon[BpfProgram, BpfProgramList]
}

type NamespaceProgramReconciler struct {
	ReconcilerCommon[BpfNsProgram, BpfNsProgramList]
}
```

Several functions were added to the bpfmanReconciler interface that are implemented by these structures.

```go
type bpfmanReconciler[T BpfProg, TL BpfProgList[T]] interface {
	// BPF Cluster of Namespaced Reconciler
	getBpfList(ctx context.Context, opts []client.ListOption) (*TL, error)
	updateBpfStatus(ctx context.Context, bpfProgram *T, condition metav1.Condition) error
	createBpfProgram(
		attachPoint string,
		rec bpfmanReconciler[T, TL],
		annotations map[string]string,
	) (*T, error)

	// *Program Reconciler
  SetupWithManager(mgr ctrl.Manager) error
	:
}
```

And the \*Programs use the `ClusterProgramReconciler` or `NamespaceProgramReconciler` structs instead
of the `ReconcilerCommon` struct.

```go
type TcProgramReconciler struct {
	ClusterProgramReconciler
	currentTcProgram *bpfmaniov1alpha1.TcProgram
	interfaces       []string
	ourNode          *v1.Node
}

type TcNsProgramReconciler struct {
	NamespaceProgramReconciler
	currentTcNsProgram *bpfmaniov1alpha1.TcNsProgram
	interfaces         []string
	ourNode            *v1.Node
}

:
```

## Naming

In the existing codebase, all the CRDs are cluster scoped:

* BpfApplicationProgram
* BpfProgram
* FentryProgram
* FexitProgram
* KprobeProgram
* ...

Common practice is for cluster scoped objects to include "Cluster" in the name and
for namespaced objects to not have an identifier.
So the current CRDs SHOULD have been named:

* ClusterBpfApplicationProgram
* ClusterBpfProgram
* ClusterFentryProgram
* ClusterFexitProgram
* ClusterKprobeProgram
* ...

Around the same time this feature is being developed, another feature is being developed
which will break the loading and attaching of eBPF programs in bpfman into two steps.
As part of this feature, all the CRDs will be completely reworked.
With this in mind, the plan for adding namespace scoped CRDs is to make the namespaced CRDs
carry the identifier.
After the load/attach split work is complete, the CRDs will be renamed to follow the common
convention in which the cluster-scoped CRD names are prefixed with "Cluster".

The current plan is for the namespaced scoped CRDs to use "<ProgramType>NsProgram" identifier and cluster
scoped CRDs to use "<ProgramType>Program" identifier.
With the new namespace scope feature, below are the list of CRDs supported by bpfman-operator:

* BpfNsApplicationProgram
* BpfApplicationProgram
* BpfNsProgram
* BpfProgram
* FentryProgram
* FexitProgram
* KprobeProgram
* TcNsProgram
* TcProgram
* TcxNsProgram
* TcxProgram
* TracepointProgram
* UprobeNsProgram
* UprobeProgram
* XdpNsProgram
* XdpProgram
