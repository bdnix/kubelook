package helm4

// WorkDir is the directory set by WithWorkDir, for the tests of the factory.
func (a *Adapter) WorkDir() string { return a.workDir }
