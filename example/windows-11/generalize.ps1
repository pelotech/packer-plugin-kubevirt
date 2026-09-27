$ErrorActionPreference = 'Stop'

# '/quit' leaves Windows running: a shutdown from the guest would make KubeVirt start it again
& "$env:SystemRoot\System32\Sysprep\Sysprep.exe" /generalize /oobe /quit /quiet /unattend:C:\Windows\Temp\unattend.xml

$stateKey = 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Setup\State'
for ($attempt = 0; $attempt -lt 90; $attempt++) {
    $state = (Get-ItemProperty $stateKey).ImageState
    Write-Output "image state: $state"
    if ($state -eq 'IMAGE_STATE_GENERALIZE_RESEAL_TO_OOBE') {
        exit 0
    }
    Start-Sleep -Seconds 10
}

Get-Content "$env:SystemRoot\System32\Sysprep\Panther\setuperr.log" -ErrorAction SilentlyContinue
exit 1
