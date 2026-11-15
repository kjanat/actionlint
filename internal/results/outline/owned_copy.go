package outline

//go:generate go run ../../development/generators/owned-copies -root ../../.. -package ./internal/results/outline -type Workflow,Action,Input,Output,CompositeRuns,DockerRuns,JavaScriptRuns,PluginRuns,UnknownRuns -output internal/results/outline/owned_copy.gen.go
