Lib sub-module
===

This sub-module is just an empty folder which job is to group other modules
shared by the agent and server CLIs. Keep in mind that other programs could use
that too (i.e. argument/flag parsing), so when you build a module keep it as
opinionated as possible. You can imagine each sub-module as a single, separated
library. This provides different benefits:
1. if the module is not needed, it is not included in the build (the executable
   will be smaller)
2. modularity means it is easier to maintain since each package module can use a
   specific version of a dependency

## I want to add another folder here, what should I do?

To add a new folder (module) you need to basically create a sub-module here.
Let's say that your module is called `X`. You will need to type:
```bash
go mod init bitbucket.org/zextras/service-discover/cli/lib/X
```

At that point, you're free to start building your module. You have to consider
it as if it is a new library project.
