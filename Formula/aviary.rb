class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.30"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.30/aviary_v0.7.30_darwin_arm64.tar.gz"
      sha256 "e1b676e0a5b46de8bed050ecb9ac505a2b2a2fb73b56d8980092a621d0faad46"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.30/aviary_v0.7.30_darwin_amd64.tar.gz"
      sha256 "f9ba27154d1fb295e06e6a5dfebd709652c13d54df59e197a0371ff5263e8732"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.30/aviary_v0.7.30_linux_arm64.tar.gz"
      sha256 "c9d8a45300f5cd8c7c5417260dc872a502249bef41d172523c4b9e85038a9617"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.30/aviary_v0.7.30_linux_amd64.tar.gz"
      sha256 "cfcb7a4c384bae8f98ffb32a45ebf952c9a825c8aa11c12a6d73fe5197ad5997"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
