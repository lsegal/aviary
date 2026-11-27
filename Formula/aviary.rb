class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "1.0.0"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v1.0.0/aviary_v1.0.0_darwin_arm64.tar.gz"
      sha256 "ba3e1ebb631de37e5b111114a18d33fdb63b220328bad3f627dda1d54e7c1b86"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v1.0.0/aviary_v1.0.0_darwin_amd64.tar.gz"
      sha256 "9ce61547c1dd4332480c7843d0978806b50041bf4f77811fac621a405fdd8caf"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v1.0.0/aviary_v1.0.0_linux_arm64.tar.gz"
      sha256 "9f2ab428960445675ed6622c587af25410097e81a1e9fcb11874a741cdde200a"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v1.0.0/aviary_v1.0.0_linux_amd64.tar.gz"
      sha256 "b62d8367269b9344bb41ff13e82eff870ce1a5ea88a79bc84a2e46ac9c9b2ec6"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
