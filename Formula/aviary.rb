class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.73"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.73/aviary_v0.7.73_darwin_arm64.tar.gz"
      sha256 "b20cdc061a5dcf4f4b044c9570c357ae44b406bd88fda975b040f340653a5c62"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.73/aviary_v0.7.73_darwin_amd64.tar.gz"
      sha256 "cc5f9fbe18f1928b7d2a36fafcb74874142c004074f133409afc97fb40c453bd"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.73/aviary_v0.7.73_linux_arm64.tar.gz"
      sha256 "0bb2c18040528c764b4f6a9ace5a74a8bc9ab01bd918553bc5a6a9fd0c3b9d18"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.73/aviary_v0.7.73_linux_amd64.tar.gz"
      sha256 "daaa99a8e19e4f7722a0844fd7136c7892514bb165527678f2e81239c856438b"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
