package templates

// WorkspaceModuleRefHelper is the Go source of workspaceModuleRef, which
// decides how a client bound to a workspace path loads its module. It uses
// only the standard library, so it can be compiled and tested on its own.
const WorkspaceModuleRefHelper = `// workspaceModuleRef returns the Git address and pin of the module at path,
// relative to the root of the repository of the workspace at address, at
// commit; or false to load the module by path. A local workspace, a workspace
// without a commit, and one that does not hold the calling module
// (holdsCaller) load by path.
func workspaceModuleRef(address, commit string, holdsCaller bool, path string) (string, string, bool) {
	path = strings.TrimPrefix(path, "/")
	if !holdsCaller || commit == "" || address == "" || path == "" || strings.HasPrefix(address, "/") || strings.HasPrefix(address, ".") {
		return "", "", false
	}
	// The selector follows the last "@" past the host, which may itself hold an
	// "@": user@host in an SCP-style address, or userinfo in a URL.
	host := 0
	if i := strings.Index(address, "://"); i >= 0 {
		host = i + len("://")
	}
	repo := address
	if end := strings.IndexAny(address[host:], "/:"); end >= 0 {
		if at := strings.LastIndex(address, "@"); at > host+end {
			repo = address[:at]
		}
	}
	if strings.Contains(repo, "://") {
		return repo + "#" + commit + ":" + path, commit, true
	}
	return repo + "/" + path + "@" + commit, commit, true
}
`

// WorkspaceModuleHelper is the Go source of the transport that a unified
// client bound to a workspace path uses, and of workspaceModuleRef. The
// generated client names no commit, so it does not change from one commit to
// the next; the transport asks the engine for the workspace at run time.
const WorkspaceModuleHelper = `// workspaceModuleClient serves the module at path, relative to the root of the
// repository that holds the calling module. When the workspace is a Git ref of
// that repository, the module is loaded as a Git module at the workspace
// commit: the engine then namespaces its cache volumes by the module's ref, as
// for its other callers, rather than by its content. Otherwise, or if that
// fails, it is loaded from the calling module's tree by path.
func workspaceModuleClient(connection *dagger.Client, path string) graphql.Client {
	return &workspaceModuleTransport{connection: connection, path: path}
}

type workspaceModuleTransport struct {
	connection *dagger.Client
	path       string
	once       sync.Once
	client     graphql.Client
}

func (t *workspaceModuleTransport) MakeRequest(ctx context.Context, req *graphql.Request, resp *graphql.Response) error {
	t.once.Do(func() {
		address, pin := t.path, ""
		if ref, refPin, ok := workspaceModuleGitRef(ctx, defaultOrExplicit(t.connection), t.path); ok {
			address, pin = ref, refPin
		}
		t.client = dagger.ModuleGraphQLClient(t.connection, address, pin)
	})
	return t.client.MakeRequest(ctx, req, resp)
}

// workspaceModuleGitRef returns the Git address of the module when the
// workspace holds the calling module, the workspace directory at the module's
// subpath having the module's own source digest, and the engine serves it from
// there. Any error, such as an engine whose module processes have no current
// module, loads by path.
func workspaceModuleGitRef(ctx context.Context, client graphql.Client, path string) (string, string, bool) {
	var ws struct {
		CurrentWorkspace struct {
			Address string
			Git     struct{ Head struct{ Commit string } }
		}
		CurrentModule struct {
			SourceSubpath string
			Source        struct{ Digest string }
		}
	}
	err := client.MakeRequest(ctx, &graphql.Request{
		OpName: "WorkspaceModule",
		Query:  "query WorkspaceModule { currentWorkspace { address git { head { commit } } } currentModule { sourceSubpath source { digest } } }",
	}, &graphql.Response{Data: &ws})
	if err != nil {
		return "", "", false
	}
	address, commit := ws.CurrentWorkspace.Address, ws.CurrentWorkspace.Git.Head.Commit
	if _, _, ok := workspaceModuleRef(address, commit, true, path); !ok {
		return "", "", false
	}
	var caller struct {
		CurrentWorkspace struct{ Directory struct{ Digest string } }
	}
	err = client.MakeRequest(ctx, &graphql.Request{
		OpName:    "WorkspaceCaller",
		Query:     "query WorkspaceCaller($path: String!) { currentWorkspace { directory(path: $path, exclude: [\"**/.git\"], gitignore: true) { digest } } }",
		Variables: map[string]any{"path": ws.CurrentModule.SourceSubpath},
	}, &graphql.Response{Data: &caller})
	holdsCaller := err == nil && ws.CurrentModule.Source.Digest != "" && caller.CurrentWorkspace.Directory.Digest == ws.CurrentModule.Source.Digest
	ref, pin, ok := workspaceModuleRef(address, commit, holdsCaller, path)
	if !ok {
		return "", "", false
	}
	err = client.MakeRequest(ctx, &graphql.Request{
		OpName:    "LoadWorkspaceModule",
		Query:     "query LoadWorkspaceModule($address: String!, $pin: String) { serveModule(address: $address, refPin: $pin) }",
		Variables: map[string]any{"address": ref, "pin": pin},
	}, &graphql.Response{})
	return ref, pin, err == nil
}

` + WorkspaceModuleRefHelper
