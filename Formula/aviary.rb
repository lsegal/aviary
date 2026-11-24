class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.24"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.24/aviary_v0.7.24_darwin_arm64.tar.gz"
      sha256 "f1e2fd049f6aca7b3248eeea8f30f1997af0a38622f11390bc43dc62df4171dc"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.24/aviary_v0.7.24_darwin_amd64.tar.gz"
      sha256 "f0d341e88f400c30270158e01cdb6b662a7c6df12363a4001ed2a56d5b5e88ff"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.24/aviary_v0.7.24_linux_arm64.tar.gz"
      sha256 "1034bc0f50ad9ac79a8d654e76cc88458c10efd72803b2cb377f97dbec353f3a"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.24/aviary_v0.7.24_linux_amd64.tar.gz"
      sha256 "c633d3d9cc46cd769237c3c2e486fa13e18b893fb386b7663cf3b0e81c653a91"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
