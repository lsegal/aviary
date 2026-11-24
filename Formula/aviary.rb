class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.13"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.13/aviary_v0.7.13_darwin_arm64.tar.gz"
      sha256 "064bdf5faa9c33659d4754bf8e455b5396e63c112c21c9548750da95ca2a1bcc"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.13/aviary_v0.7.13_darwin_amd64.tar.gz"
      sha256 "403b01712a16f880d23899835fc9682a119c060722d87aedbb9ec43cde2af9fb"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.13/aviary_v0.7.13_linux_arm64.tar.gz"
      sha256 "c1c52a62c26ec54e9db93668f1eed3bedbaa2df42d7ef77ddcac4fc01718a52e"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.13/aviary_v0.7.13_linux_amd64.tar.gz"
      sha256 "63b43ec159c1f2c0e3b37da8c701d61c93ab37fbd5dd62d0d8e260cda7992c2f"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
