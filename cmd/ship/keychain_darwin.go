//go:build darwin && cgo

package main

/*
#cgo LDFLAGS: -framework Security -framework CoreFoundation
#include <Security/Security.h>
#include <CoreFoundation/CoreFoundation.h>
#include <stdlib.h>

static CFMutableDictionaryRef shipKeychainQuery(const char *account) {
    CFMutableDictionaryRef query = CFDictionaryCreateMutable(NULL, 0,
        &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
    CFStringRef name = CFStringCreateWithCString(NULL, account, kCFStringEncodingUTF8);
    CFDictionarySetValue(query, kSecClass, kSecClassGenericPassword);
    CFDictionarySetValue(query, kSecAttrService, CFSTR("com.github.unix2dos.ship"));
    CFDictionarySetValue(query, kSecAttrAccount, name);
    CFDictionarySetValue(query, kSecAttrSynchronizable, kCFBooleanFalse);
    CFDictionarySetValue(query, kSecUseAuthenticationUI, kSecUseAuthenticationUIFail);
    CFRelease(name);
    return query;
}

static OSStatus shipKeychainPut(const char *account, const void *bytes, long size) {
    CFMutableDictionaryRef query = shipKeychainQuery(account);
    CFDataRef data = CFDataCreate(NULL, bytes, size);
    CFDictionarySetValue(query, kSecValueData, data);
    OSStatus status = SecItemAdd(query, NULL);
    CFRelease(data);
    CFRelease(query);
    return status;
}

static OSStatus shipKeychainGet(const char *account, CFTypeRef *result) {
    CFMutableDictionaryRef query = shipKeychainQuery(account);
    CFDictionarySetValue(query, kSecReturnData, kCFBooleanTrue);
    CFDictionarySetValue(query, kSecMatchLimit, kSecMatchLimitOne);
    OSStatus status = SecItemCopyMatching(query, result);
    CFRelease(query);
    return status;
}

static OSStatus shipKeychainDelete(const char *account) {
    CFMutableDictionaryRef query = shipKeychainQuery(account);
    OSStatus status = SecItemDelete(query);
    CFRelease(query);
    return status;
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

func keychainError(status C.OSStatus) error {
	if status == C.errSecSuccess {
		return nil
	}
	return fmt.Errorf("macOS Keychain access failed (status %d); unlock or authorize Keychain access before retrying; no plaintext fallback", int(status))
}

func keychainPut(ref string, value []byte) error {
	account := C.CString(ref)
	defer C.free(unsafe.Pointer(account))
	if len(value) == 0 {
		return fmt.Errorf("empty secret is not allowed")
	}
	return keychainError(C.shipKeychainPut(account, unsafe.Pointer(&value[0]), C.long(len(value))))
}

func keychainGet(ref string) ([]byte, error) {
	account := C.CString(ref)
	defer C.free(unsafe.Pointer(account))
	var result C.CFTypeRef
	if err := keychainError(C.shipKeychainGet(account, &result)); err != nil {
		return nil, err
	}
	defer C.CFRelease(result)
	if C.CFGetTypeID(result) != C.CFDataGetTypeID() {
		return nil, fmt.Errorf("Keychain returned an unexpected secret type")
	}
	data := C.CFDataRef(result)
	size := C.CFDataGetLength(data)
	if size <= 0 || size > maxSecretSize {
		return nil, fmt.Errorf("Keychain secret size is invalid")
	}
	return C.GoBytes(unsafe.Pointer(C.CFDataGetBytePtr(data)), C.int(size)), nil
}

func keychainDelete(ref string) error {
	account := C.CString(ref)
	defer C.free(unsafe.Pointer(account))
	return keychainError(C.shipKeychainDelete(account))
}
