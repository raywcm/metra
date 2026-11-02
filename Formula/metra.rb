class Metra < Formula
  desc "Lightweight terminal system monitor"
  homepage "https://github.com/raywcm/metra"
  version "0.7.3"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/raywcm/metra/releases/download/v0.7.3/metra_0.7.3_darwin_arm64.tar.gz"
      sha256 "0d285bfa2b580bfdb3fa92fe76d196e05577b195c9b927fb878f1000f0089484"
    end
    on_intel do
      url "https://github.com/raywcm/metra/releases/download/v0.7.3/metra_0.7.3_darwin_amd64.tar.gz"
      sha256 "848c26f4d8e97e20764e56461d98b91a5a2d467e35c4540bf870fcabb7c1d360"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/raywcm/metra/releases/download/v0.7.3/metra_0.7.3_linux_arm64.tar.gz"
      sha256 "b54da2e8a1a5b8c31ae30b2d1de2b803b2ed46817eb3c1d0cfdbe68f02732cb1"
    end
    on_intel do
      url "https://github.com/raywcm/metra/releases/download/v0.7.3/metra_0.7.3_linux_amd64.tar.gz"
      sha256 "f781c6db38691776540b3a4af4d43cbaa1067e0af04d505f0bf18d09007c2f4e"
    end
  end

  def install
    bin.install "metra"
  end

  test do
    assert_match "metra", shell_output("#{bin}/metra --version")
  end
end
