class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.78"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.78/aviary_v0.7.78_darwin_arm64.tar.gz"
      sha256 "b60241d872071d0d78f48b0ec5734106b2361305f817d6ee60eccbb3f63162cb"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.78/aviary_v0.7.78_darwin_amd64.tar.gz"
      sha256 "968e9d857f6bcac0baed5fb14607e634ccdde6ab9e0122c47464fcc6e85c1da9"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.78/aviary_v0.7.78_linux_arm64.tar.gz"
      sha256 "a864fdd1efb5a99a8033a8a3299109e0082486b52c927fc39a1fa98d4af39163"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.78/aviary_v0.7.78_linux_amd64.tar.gz"
      sha256 "a58bd6d621534ea747689be9689f7eacade26e759edcf8b3d01472dc241db209"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
