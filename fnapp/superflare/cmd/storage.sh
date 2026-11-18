#!/bin/bash

storage_error() {
    printf '%s\n' "$*" >&2
    if [ -n "${TRIM_TEMP_LOGFILE:-}" ]; then
        printf '%s\n' "$*" >> "${TRIM_TEMP_LOGFILE}" 2>/dev/null || true
    fi
    return 1
}

prepare_shared_storage_locked() {
    local marker="${STORAGE_DIR}/.shared-storage"
    local stage
    local legacy
    local canonical

    if [ -e "${STORAGE_DIR}" ] || [ -L "${STORAGE_DIR}" ]; then
        if [ -L "${STORAGE_DIR}" ] || [ ! -d "${STORAGE_DIR}" ] ||
           [ ! -f "${marker}" ] || [ -L "${marker}" ] ||
           [ "$(cat "${marker}")" != "superflare-shared-storage-v1" ]; then
            storage_error "Shared data/ already exists without a completed migration; refusing to overwrite it."
            return 1
        fi
        for canonical in "${ETC_DIR}" "${VAR_DIR}"; do
            if [ ! -d "${canonical}" ] || [ -L "${canonical}" ]; then
                storage_error "Shared etc/ or var/ is missing or invalid; restore it before starting SuperFlare."
                return 1
            fi
        done
        return 0
    fi

    for legacy in "${PKG_ETC_DIR}" "${PKG_VAR_DIR}"; do
        [ -n "${legacy}" ] || return 1
        if [ -e "${legacy}" ] || [ -L "${legacy}" ]; then
            canonical="$(CDPATH= cd -- "${legacy}" 2>/dev/null && pwd -P)" || return 1
            case "${SHARE_DIR}/" in
                "${canonical}/"*) storage_error "Legacy and shared storage must be separate directories."; return 1 ;;
            esac
            case "${canonical}/" in
                "${SHARE_DIR}/"*) storage_error "Legacy and shared storage must be separate directories."; return 1 ;;
            esac
        fi
    done
    if status_app; then
        storage_error "Stop the previous SuperFlare service before migrating its data."
        return 1
    fi

    stage="$(mktemp -d "${SHARE_DIR}/.data-migration.XXXXXX")" || return 1
    (
        mkdir -p "${stage}/etc" "${stage}/var" || exit 1
        if [ -d "${PKG_ETC_DIR}" ]; then
            cp -a "${PKG_ETC_DIR}/." "${stage}/etc/" || exit 1
        fi
        if [ -d "${PKG_VAR_DIR}" ]; then
            cp -a "${PKG_VAR_DIR}/." "${stage}/var/" || exit 1
        fi
        printf '%s\n' 'superflare-shared-storage-v1' > "${stage}/.shared-storage" || exit 1
        mv -T "${stage}" "${STORAGE_DIR}"
    ) || {
        # Only our unpublished staging directory is disposable; keep both sources.
        rm -rf -- "${stage}"
        storage_error "Could not copy legacy SuperFlare data into the application shared folder."
        return 1
    }
}

prepare_shared_storage() {
    case "${SHARE_DIR}" in
        *:*) storage_error "SuperFlare requires exactly one application shared folder."; return 1 ;;
    esac
    if [ ! -d "${SHARE_DIR}" ]; then
        storage_error "SuperFlare shared folder is unavailable; check application file permissions or reinstall the package."
        return 1
    fi
    (
        if command -v flock >/dev/null 2>&1; then
            flock -x 8 || exit 1
        fi
        prepare_shared_storage_locked
    ) 8>"${SHARE_DIR}/.superflare-storage.lock"
}
