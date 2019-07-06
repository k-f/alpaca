package main

import (
	"github.com/keybase/go-keychain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"io/ioutil"
	"os"
	"path"
	"testing"
)

func TestNoMAD(t *testing.T) {
	dir, err := ioutil.TempDir("", "alpaca")
	require.Nil(t, err)
	defer os.RemoveAll(dir)
	kc, err := keychain.NewKeychain(path.Join(dir, "test.keychain"), "")
	require.Nil(t, err)
	testKeychain = &kc

	passwd := keychain.NewGenericPassword("", "malory@ISIS", "NoMAD", []byte("guest"), "")
	passwd.SetAccessible(keychain.AccessibleWhenUnlocked)
	passwd.UseKeychain(kc)
	require.Nil(t, keychain.AddItem(passwd))

	query := keychain.NewItem()
	query.SetMatchSearchList(kc)
	query.SetSecClass(keychain.SecClassGenericPassword)
	query.SetAccount("malory@ISIS")
	query.SetReturnAttributes(true)
	query.SetReturnData(true)
	results, err := keychain.QueryItem(query)
	require.Nil(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "NoMAD", results[0].Label)
	assert.Equal(t, []byte("guest"), results[0].Data)
}
