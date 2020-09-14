package actions

import (
	"github.com/farmersedgeinc/yaml-crypt/pkg/yaml"
	"github.com/farmersedgeinc/yaml-crypt/pkg/crypto"
)

func Decrypt(f *File, p *crypto.Provider, plain bool, stdout bool, threads uint) error {
	// read in files
	var encryptedPath, decryptedPath, plainPath string
	var err error
	if stdout {
		encryptedPath = f.Path
	} else {
		encryptedPath, decryptedPath, plainPath, err = f.AllPaths()
		if err != nil { return err }
	}
	decryptedValues := map[string] *yaml.DecryptedValue{}
	if exists(decryptedPath) {
		_, decryptedValues, err = yaml.ReadDecryptedFile(decryptedPath)
		if err != nil { return err }
	}
	node, encryptedValues, err := yaml.ReadEncryptedFile(encryptedPath)
	if err != nil { return err }

	// spin up workers
	jobs := make(chan job)
	errors := make(chan error)
	for i := uint(0); i < threads; i++ {
		go decryptWorker(jobs, errors, p, !plain)
	}

	// create thread to give workers jobs
	go func() {
		for path, encryptedValue := range encryptedValues {
			jobs <- job{d: decryptedValues[path], e: encryptedValue, path: path}
		}
		close(jobs)
	}()

	// consume errors
	for i := 0; i < len(encryptedValues); i++ {
		err := <-errors
		if err != nil {
			return err
		}
	}

	// write output
	var outPath string
	if stdout {
		outPath = ""
	} else if plain {
		outPath = plainPath
	} else {
		outPath = decryptedPath
	}
	return yaml.SaveFile(outPath, node)
}

func Encrypt(f *File, p *crypto.Provider, threads uint) error {
	// read in files
	encryptedPath, decryptedPath, _, err := f.AllPaths()
	if err != nil { return err }
	encryptedValues := map[string] *yaml.EncryptedValue{}
	if exists(encryptedPath) {
		_, encryptedValues, err = yaml.ReadEncryptedFile(encryptedPath)
		if err != nil { return err }
	}
	node, decryptedValues, err := yaml.ReadDecryptedFile(decryptedPath)
	if err != nil { return err }

	// spin up workers
	jobs := make(chan job)
	errors := make(chan error)
	for i := uint(0); i < threads; i++ {
		go encryptWorker(jobs, errors, p)
	}

	// create thread to give workers jobs
	go func() {
		for path, decryptedValue := range decryptedValues {
			jobs <- job{d: decryptedValue, e: encryptedValues[path], path: path}
		}
		close(jobs)
	}()

	// consume errors
	for i := 0; i < len(decryptedValues); i++ {
		err := <-errors
		if err != nil {
			return err
		}
	}

	// write output
	err = yaml.SaveFile(encryptedPath, node)
	return err
}

type job struct {
	d *yaml.DecryptedValue
	e *yaml.EncryptedValue
	path string
}

func decryptWorker(jobs <-chan job, errs chan<- error, p *crypto.Provider, tag bool) {
	var err error
	for job := range jobs {
		if job.d != nil && job.e.Compare(job.d) {
			job.d.Node = job.e.Node
		} else {
			job.d, err = job.e.Decrypt(*p, tag)
		}
		if err == nil {
			job.d.ReplaceNode()
		}
		errs <- err
	}
}

func encryptWorker(jobs <-chan job, errs chan<- error, p *crypto.Provider) {
	var err error
	for job := range jobs {
		if job.e != nil && job.e.Compare(job.d) {
			job.e.Node = job.d.Node
		} else {
			job.e, err = job.d.Encrypt(*p)
		}
		if err == nil {
			job.e.ReplaceNode()
		}
		errs <- err
	}
}
