# Changelog

## [0.2.0](https://github.com/pelotech/packer-plugin-kubevirt/compare/v0.1.0...v0.2.0) (2026-09-28)


### Features

* build Windows images from an install ISO with UEFI ([#97](https://github.com/pelotech/packer-plugin-kubevirt/issues/97)) ([ce7b780](https://github.com/pelotech/packer-plugin-kubevirt/commit/ce7b7800945388ced33bb7728e939cc2fa84cf4c))
* clearer setting names, grouped by prefix ([#115](https://github.com/pelotech/packer-plugin-kubevirt/issues/115)) ([0caea8b](https://github.com/pelotech/packer-plugin-kubevirt/commit/0caea8bf7432d4c5133b4fdfac57d4a2c735d52a))
* generalize in its own step, with an option to skip virt-sysprep ([#107](https://github.com/pelotech/packer-plugin-kubevirt/issues/107)) ([8051a18](https://github.com/pelotech/packer-plugin-kubevirt/commit/8051a1800fee00d46c58956666f68d4467cea7e1))
* set the lifetime of the export with vm_export_ttl ([#114](https://github.com/pelotech/packer-plugin-kubevirt/issues/114)) ([142eabe](https://github.com/pelotech/packer-plugin-kubevirt/commit/142eabe031c8f30eed2b81fe330c99ad54d99759))


### Bug Fixes

* check the required settings of the builder ([#117](https://github.com/pelotech/packer-plugin-kubevirt/issues/117)) ([1e41245](https://github.com/pelotech/packer-plugin-kubevirt/commit/1e412450e21e9cae7834dc6a28d76a49ca96baa3))
* fail the S3 download on an HTTP error ([#119](https://github.com/pelotech/packer-plugin-kubevirt/issues/119)) ([54f9f2c](https://github.com/pelotech/packer-plugin-kubevirt/commit/54f9f2c4c2d78d11812d5307da51be4d214bc1b4))
* give each upload Job its own name ([#113](https://github.com/pelotech/packer-plugin-kubevirt/issues/113)) ([71685f8](https://github.com/pelotech/packer-plugin-kubevirt/commit/71685f86e20cb0902b7546948fc1b36f6e27433a))
* keep the disk until its export is deleted ([#110](https://github.com/pelotech/packer-plugin-kubevirt/issues/110)) ([7b402c2](https://github.com/pelotech/packer-plugin-kubevirt/commit/7b402c204225a87e1fa9acd9af748d0c81a956a6))
* keep waiting when a watch closes and stop when the build is cancelled ([#120](https://github.com/pelotech/packer-plugin-kubevirt/issues/120)) ([52c3129](https://github.com/pelotech/packer-plugin-kubevirt/commit/52c3129edf9f024d47e7cadad85e408a23ad94d8))
* return an error when the build is cancelled or halted ([#109](https://github.com/pelotech/packer-plugin-kubevirt/issues/109)) ([4b5f93a](https://github.com/pelotech/packer-plugin-kubevirt/commit/4b5f93a27735203e4799c1e2101d5823061e3871))
