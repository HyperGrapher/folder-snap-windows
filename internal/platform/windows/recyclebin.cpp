#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <shellapi.h>
#include <shobjidl.h>
#include <cstdio>

extern "C" int foldersnap_recycle(const wchar_t* path, wchar_t* message, unsigned int messageLength) {
    if (!path || !*path) {
        if (message && messageLength) wcsncpy_s(message, messageLength, L"Empty cleanup path", _TRUNCATE);
        return E_INVALIDARG;
    }

    HRESULT init = CoInitializeEx(nullptr, COINIT_APARTMENTTHREADED | COINIT_DISABLE_OLE1DDE);
    const bool uninitialize = SUCCEEDED(init);
    if (FAILED(init) && init != RPC_E_CHANGED_MODE) {
        if (message && messageLength) swprintf_s(message, messageLength, L"COM initialization failed (0x%08lx)", init);
        return static_cast<int>(init);
    }

    IFileOperation* operation = nullptr;
    IShellItem* item = nullptr;
    HRESULT hr = CoCreateInstance(CLSID_FileOperation, nullptr, CLSCTX_INPROC_SERVER, IID_PPV_ARGS(&operation));
    if (SUCCEEDED(hr)) {
        DWORD flags = static_cast<DWORD>(
            FOF_ALLOWUNDO | FOF_NOCONFIRMATION | FOF_NOERRORUI | FOF_SILENT |
            FOFX_RECYCLEONDELETE | FOFX_EARLYFAILURE);
        hr = operation->SetOperationFlags(flags);
    }
    if (SUCCEEDED(hr)) hr = SHCreateItemFromParsingName(path, nullptr, IID_PPV_ARGS(&item));
    if (SUCCEEDED(hr)) hr = operation->DeleteItem(item, nullptr);
    if (SUCCEEDED(hr)) hr = operation->PerformOperations();
    if (SUCCEEDED(hr)) {
        BOOL aborted = FALSE;
        hr = operation->GetAnyOperationsAborted(&aborted);
        if (SUCCEEDED(hr) && aborted) hr = HRESULT_FROM_WIN32(ERROR_CANCELLED);
    }

    if (item) item->Release();
    if (operation) operation->Release();
    if (uninitialize) CoUninitialize();
    if (FAILED(hr) && message && messageLength) {
        swprintf_s(message, messageLength, L"Recycle Bin operation failed (0x%08lx)", hr);
    }
    return FAILED(hr) ? static_cast<int>(hr) : 0;
}
