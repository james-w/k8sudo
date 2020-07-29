k8sudo, elevate permissions against the k8s API
===============================================

When running a Kubernetes cluster you don't want to have users
running as cluster-admin all the time as mistakes can have
disastrous consequences.

If you give users restricted permissions so that they can't
break anything if they make a mistake then you have a problem
when they do need to do something with elevated permissions,
for instance when dealing with an outage.

This can lead to a difficult choice between giving users
elevated permissions to deal with unexpected situations
independently and giving them restrictive permissions so that
mistakes have a limited blast radius.

This is similar to the situation on UNIX-like systems
where some operations are limited to the root user. Running
as root is a bad idea, so there needs to be a way for normal
users to escalate permissions to run some processes as root.
In that context there is `sudo` that allows for defining
rules about who is allowed to run which commands as root,
with audit logs to record when they used those privileges.

This project aims to offer a similar capability to Kuberenetes.
It adds the `SudoRequest` API type that allows for users to
request escalation to a higher set of privileges for a limited
time. A user can create a request to be granted access to a
`ClusterRole` for a limited time. When the request is approved
it creates a `ClusterRoleBinding` that grants that access,
and then deletes it after it has expired.

The request is approved using Kubernetes RBAC. It adds the
`sudo` verb that can be used in a `ClusterRole`, and you can
then define roles that allow for users to access `ClusterRoles`
of your choice.

Example
-------

In this example there is a team of developers that have an
app deployed on a Kubernetes cluster.

Firstly we create a `ClusterRole` named `appdev-read` for normal
developer use, this grants read-only access.

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: appdev-sudo
rules:
– apiGroups: [“*”]
  resources:
  – deployments
  – configmaps
  – pods
  - services
  verbs:
  – get
  – list
  – watch
```

Then we create a `ClusterRoleBinding` that grants each developer
access to the `appdev-read` role.

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: devs-appdev-read
roleRef:
  apiGroup: "rbac.authorization.k8s.io"
  kind: "ClusterRole"
  name: "appdev-read"
subjects:
- apiGroup: "rbac.authorization.k8s.io"
  kind: "User"
  name: "dev1"
...
```

So far so normal.

Now we create a `ClusterRole` named `appdev-write` for the
unusual cases where the developers need to do more than read.
This gives write access to some resources.

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: appdev-write
rules:
– apiGroups: [“*”]
  resources:
  – deployments
  – configmaps
  – pods
  - services
  verbs:
  – create
  – update
  – patch
  - delete
```

Rather than creating a `ClusterRoleBinding` that grants access
to this `ClusterRole`, which would give the developers those
permissions permanently, we instead create another `ClusterRole`
named `appdev-sudo`. This grants the `sudo` verb against
the `appdev-write` `ClusterRole`

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: appdev-sudo
rules:
- apiGroups: ["rbac.authorization.k8s.io"]
  resources: ["clusterroles"]
  verbs: ["sudo"]
  resourceNames: ["appdev-write"]
```

We also have to grant the developers the ability to create
`SudoRequests`.

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: sudorequest-editor-role
rules:
- apiGroups:
  - k8sudo.jetstack.io
  resources:
  - sudorequests
  verbs:
  - create
  - delete
  - get
  - list
  - patch
  - update
  - watch
- apiGroups:
  - k8sudo.jetstack.io
  resources:
  - sudorequests/status
  verbs:
  - get
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: devs-sudorequest-editors
roleRef:
  apiGroup: "rbac.authorization.k8s.io"
  kind: "ClusterRole"
  name: "sudorequest-editor-role"
subjects:
- apiGroup: "rbac.authorization.k8s.io"
  kind: "User"
  name: "dev1"
...
```

Now the developers have read-only permissions, but if they
need write access for some reason they can temporarily request
access.

```yaml
apiVersion: k8sudo.jetstack.io/v1alpha1
kind: SudoRequest
metadata:
  name: dev1-write-request-202007291623
spec:
  user: dev1
  role: appdev-write
```

This request will then be processed, and if everything is OK then
a temporary `ClusterRoleBinding` will be created that will grant
`dev1` the permissions in `appdev-write` until it expires.

Security considerations
-----------------------

There are a couple of security considerations that should be taken
in to account when setting up a policy:

1. If the role that a user can assume gives permissions to create/edit
`ClusterRoleBindings` or `RoleBindings` then they will be able to
elevate their permissions permanently.

2. If the role that a user can assume gives permissions to alter the
`SudoRequests` controller than the user could disable the `SudoRequests`
controller entirely. The fact that the `ClusterRoleBinding` expires is
only because the `SudoRequests` controller deletes it, so if the user
can stop that happening they could permanently elevate their permissions.
