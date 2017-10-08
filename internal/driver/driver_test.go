package driver

// make sure Registry implements Driver; this won't compile otherwise:
var testRegistryImplsDriver Driver = make(Registry)
