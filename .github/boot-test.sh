#!/usr/bin/env bash
# Helpers of the boot tests of .github/workflows/tests.yml, for a Virtual Machine of the packer-linux namespace.
# Usage: boot-test.sh stop|wait-vmi|wait-os|evidence|dump <vm name>
set -euo pipefail

namespace=packer-linux
command="$1"
vm="$2"

launcher_pod() {
  kubectl get pods --namespace "${namespace}" --selector "vm.kubevirt.io/name=${vm}" --output name | head -n 1
}

# the serial console, without the terminal escape sequences of the firmware
console_log() {
  local pod
  pod="$(launcher_pod)"
  if [ -z "${pod}" ]; then
    echo "no virt-launcher pod for ${vm}"
    return 0
  fi
  if kubectl get "${pod}" --namespace "${namespace}" --output jsonpath='{.spec.initContainers[*].name} {.spec.containers[*].name}' | grep -qw guest-console-log; then
    kubectl logs "${pod}" --namespace "${namespace}" --container guest-console-log | sed 's/\x1b\[[0-9;?]*[A-Za-z]//g; s/\r//g' || true
  else
    echo "no guest-console-log container, reading the serial console for 15 seconds"
    (printf '\n'; sleep 20) | timeout 15 virtctl console "${vm}" --namespace "${namespace}" | sed 's/\x1b\[[0-9;?]*[A-Za-z]//g; s/\r//g' || true
  fi
}

domain_xml() {
  local pod
  pod="$(launcher_pod)"
  kubectl exec "${pod}" --namespace "${namespace}" --container compute -- \
    virsh --connect 'qemu+unix:///session?socket=/var/run/libvirt/virtqemud-sock' dumpxml "${namespace}_${vm}"
}

wait_until() {
  local deadline=$((SECONDS + $1))
  shift
  until "$@"; do
    if [ "${SECONDS}" -ge "${deadline}" ]; then
      echo "timed out waiting for: $*"
      return 1
    fi
    sleep 2
  done
}

vmi_gone() {
  ! kubectl get virtualmachineinstance "${vm}" --namespace "${namespace}" > /dev/null 2>&1 &&
    [ -z "$(launcher_pod)" ]
}

vmi_exists() {
  kubectl get virtualmachineinstance "${vm}" --namespace "${namespace}" > /dev/null 2>&1
}

os_known() {
  [ -n "$(kubectl get virtualmachineinstance "${vm}" --namespace "${namespace}" --output jsonpath='{.status.guestOSInfo.id}')" ]
}

case "${command}" in
  stop)
    kubectl delete virtualmachine "${vm}" --namespace "${namespace}" --ignore-not-found --wait=true --timeout=5m
    wait_until 300 vmi_gone
    ;;
  wait-vmi)
    wait_until 600 vmi_exists
    ;;
  wait-os)
    wait_until 120 os_known
    ;;
  evidence)
    vmi="$(kubectl get virtualmachineinstance "${vm}" --namespace "${namespace}" --output json)"
    echo "== ${vm}: preference, firmware, SMM, TPM and readiness probe of the VMI spec"
    echo "${vmi}" | jq '{
      preference: (.metadata.annotations // {} | with_entries(select(.key | test("preference")))),
      firmware: .spec.domain.firmware,
      smm: .spec.domain.features.smm,
      tpm: .spec.domain.devices.tpm,
      readinessProbe: .spec.readinessProbe
    }'
    echo "== ${vm}: VMI conditions"
    echo "${vmi}" | jq -r '.status.conditions[]? | "\(.type)=\(.status) \(.reason // "") \(.message // "")"'
    echo "== ${vm}: guestOSInfo"
    echo "${vmi}" | jq '.status.guestOSInfo | {id, name, prettyName, versionId, kernelRelease}'
    echo "== ${vm}: persistent state volumes"
    echo "${vmi}" | jq '[.status.volumeStatus[]? | select(.persistentVolumeClaimInfo != null) | {name, claimName: .persistentVolumeClaimInfo.claimName}]'
    kubectl get persistentvolumeclaims --namespace "${namespace}" --selector persistent-state-for="${vm}" || true
    echo "== ${vm}: firmware, SMM and TPM of the libvirt domain"
    { domain_xml | grep -E '<os|<type|<loader|<nvram|<smm|<tpm|<backend|</os>|firmware'; } || echo "domain XML not readable"
    echo "== ${vm}: boot lines of the serial console"
    { console_log | grep -a -E -i 'seabios|ovmf|edk ?ii|bdsdxe|efi|secure ?boot|shim|access denied|violation|tpm|booting|ubuntu 26' | head -n 80; } || true
    ;;
  dump)
    echo "== ${vm}: VirtualMachine"
    kubectl get virtualmachine "${vm}" --namespace "${namespace}" --output json | jq '.status' || true
    echo "== ${vm}: VirtualMachineInstance"
    kubectl get virtualmachineinstance "${vm}" --namespace "${namespace}" --output json | jq '{spec: .spec.domain, status: .status}' || true
    echo "== ${vm}: events"
    kubectl get events --namespace "${namespace}" --sort-by=.lastTimestamp | tail -n 60 || true
    kubectl get persistentvolumeclaims,datavolumes,pods --namespace "${namespace}" --output wide || true
    pod="$(launcher_pod)"
    if [ -n "${pod}" ]; then
      echo "== ${vm}: ${pod}"
      kubectl describe "${pod}" --namespace "${namespace}" || true
      echo "== ${vm}: virt-launcher log"
      kubectl logs "${pod}" --namespace "${namespace}" --container compute --tail 300 || true
      echo "== ${vm}: libvirt domain"
      domain_xml || true
    fi
    echo "== ${vm}: serial console"
    console_log | tail -n 300 || true
    ;;
  *)
    echo "unknown command ${command}"
    exit 1
    ;;
esac
