class Metra < Formula
  desc "Lightweight terminal system monitor"
  homepage "https://github.com/raywcm/metra"
  version "0.7.1"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/raywcm/metra/releases/download/v0.7.1/metra_0.7.1_darwin_arm64.tar.gz"
      sha256 "d3e316bf846499a47a605d543767c367e1b9fff33fbad8edcc840769e54eb11b"
    end
    on_intel do
      url "https://github.com/raywcm/metra/releases/download/v0.7.1/metra_0.7.1_darwin_amd64.tar.gz"
      sha256 "ccc1fedb465929c7c538820aa1dc0b9c97d965b215ba3edae4b3e2c0c3af0c7d"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/raywcm/metra/releases/download/v0.7.1/metra_0.7.1_linux_arm64.tar.gz"
      sha256 "df9516f3e2f09df969f768a01ef1374327c608be7606c08cead2a80e5c406908"
    end
    on_intel do
      url "https://github.com/raywcm/metra/releases/download/v0.7.1/metra_0.7.1_linux_amd64.tar.gz"
      sha256 "c9787940bf055027d3214df67c39d795c38590a1c6dc6f6216924c76f2c5ee06"
    end
  end

  def install
    bin.install "metra"
  end

  test do
    assert_match "metra", shell_output("#{bin}/metra --version")
  end
end
